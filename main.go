package main

import (
	"context"
	"fmt"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/subscriptions"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/jessevdk/go-flags"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"github.com/webdevops/azure-janitor/config"
	"net/http"
	"os"
	"path"
	"runtime"
	"strings"
)

const (
	Author = "webdevops.io"
)

var (
	argparser *flags.Parser
	opts      config.Opts

	azureAuthorizer    autorest.Authorizer
	azureSubscriptions []subscriptions.Subscription

	azureEnvironment azure.Environment //nolint:golint,unused

	// Git version information
	gitCommit = "<unknown>"
	gitTag    = "<unknown>"

	Prometheus struct {
		MetricDuration        *prometheus.GaugeVec
		MetricTtlResources    *prometheus.GaugeVec
		MetricDeletedResource *prometheus.CounterVec
		MetricErrors          *prometheus.CounterVec
	}
)

func main() {
	initArgparser()

	log.Infof("starting azure-janitor v%s (%s; %s; by %v)", gitTag, gitCommit, runtime.Version(), Author)
	log.Info(string(opts.GetJson()))

	log.Infof("init Azure connection")
	initAzureConnection()
	initMetricCollector()

	log.Infof("init Janitor")

	if !opts.Janitor.DisableResourceGroups {
		log.Infof("enabled Azure ResourceGroups cleanup (filter: %s)", opts.Janitor.FilterResourceGroups)
	} else {
		log.Infof("disabled Azure ResourceGroups cleanup")
	}

	if !opts.Janitor.DisableResources {
		log.Infof("enabled Azure Resources cleanup (filter: %s)", opts.Janitor.FilterResources)
	} else {
		log.Infof("disabled Azure Resources cleanup")
	}

	if !opts.Janitor.DisableDeployments {
		log.Infof("enabled Azure ResourceGroups Deployments cleanup (limit: %v, ttl: %v)", opts.Janitor.DeploymentsLimit, opts.Janitor.DeploymentsTtl.String())
	} else {
		log.Infof("disabled Azure ResourceGroups Deployments cleanup")
	}

	j := Janitor{}
	j.Init()
	j.Run()

	log.Infof("starting http server on %s", opts.ServerBind)
	startHttpServer()
}

// init argparser and parse/validate arguments
func initArgparser() {

	argparser = flags.NewParser(&opts, flags.Default)
	_, err := argparser.Parse()

	// check if there is an parse error
	if err != nil {
		if flagsErr, ok := err.(*flags.Error); ok && flagsErr.Type == flags.ErrHelp {
			os.Exit(0)
		} else {
			fmt.Println()
			argparser.WriteHelp(os.Stdout)
			os.Exit(1)
		}
	}

	// verbose level
	if opts.Logger.Verbose {
		log.SetLevel(log.DebugLevel)
	}

	// debug level
	if opts.Logger.Debug {
		log.SetReportCaller(true)
		log.SetLevel(log.TraceLevel)
		log.SetFormatter(&log.TextFormatter{
			CallerPrettyfier: func(f *runtime.Frame) (string, string) {
				s := strings.Split(f.Function, ".")
				funcName := s[len(s)-1]
				return funcName, fmt.Sprintf("%s:%d", path.Base(f.File), f.Line)
			},
		})
	}

	// json log format
	if opts.Logger.LogJson {
		log.SetReportCaller(true)
		log.SetFormatter(&log.JSONFormatter{
			DisableTimestamp: true,
			CallerPrettyfier: func(f *runtime.Frame) (string, string) {
				s := strings.Split(f.Function, ".")
				funcName := s[len(s)-1]
				return funcName, fmt.Sprintf("%s:%d", path.Base(f.File), f.Line)
			},
		})
	}

	// resourceGroup filter
	opts.Janitor.FilterResourceGroups = fmt.Sprintf(
		"tagName eq '%s'",
		strings.Replace(opts.Janitor.Tag, "'", "\\'", -1),
	)

	// add additional filter
	if opts.Janitor.AdditionalFilterResourceGroups != nil {
		opts.Janitor.FilterResourceGroups = fmt.Sprintf(
			"%s and %s",
			opts.Janitor.FilterResourceGroups,
			*opts.Janitor.AdditionalFilterResourceGroups,
		)
	}

	// resource (if we specify tagValue here we don't get the tag.. wtf?!)
	opts.Janitor.FilterResources = ""

	// add additional filter
	if opts.Janitor.AdditionalFilterResources != nil {
		opts.Janitor.FilterResources = *opts.Janitor.AdditionalFilterResources
	}
}

// Init and build Azure authorzier
func initAzureConnection() {
	var err error
	ctx := context.Background()

	// get environment
	azureEnvironment, err = azure.EnvironmentFromName(*opts.Azure.Environment)
	if err != nil {
		log.Panic(err)
	}

	// setup azure authorizer
	azureAuthorizer, err = auth.NewAuthorizerFromEnvironment()
	if err != nil {
		panic(err)
	}
	subscriptionsClient := subscriptions.NewClient()
	subscriptionsClient.Authorizer = azureAuthorizer

	if len(opts.Azure.Subscription) == 0 {
		// auto lookup subscriptions
		listResult, err := subscriptionsClient.List(ctx)
		if err != nil {
			panic(err)
		}
		azureSubscriptions = listResult.Values()

		if len(azureSubscriptions) == 0 {
			log.Panic("no Azure Subscriptions found via auto detection, does this ServicePrincipal have read permissions to the subcriptions?")
		}
	} else {
		// fixed subscription list
		azureSubscriptions = []subscriptions.Subscription{}
		for _, subId := range opts.Azure.Subscription {
			result, err := subscriptionsClient.Get(ctx, subId)
			if err != nil {
				panic(err)
			}
			azureSubscriptions = append(azureSubscriptions, result)
		}
	}
}

func initMetricCollector() {

	Prometheus.MetricDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "azurejanitor_duration",
			Help: "AzureJanitor cleanup duration",
		},
		[]string{},
	)

	Prometheus.MetricTtlResources = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "azurejanitor_resources_ttl",
			Help: "AzureJanitor number of resources with TTL",
		},
		[]string{
			"resourceID",
			"subscriptionID",
			"resourceGroup",
			"provider",
		},
	)

	Prometheus.MetricDeletedResource = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "azurejanitor_resources_deleted",
			Help: "AzureJanitor deleted resources",
		},
		[]string{
			"resourceType",
		},
	)
	Prometheus.MetricErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "azurejanitor_errors",
			Help: "AzureJanitor error counter",
		},
		[]string{
			"resourceType",
		},
	)

	prometheus.MustRegister(Prometheus.MetricDuration)
	prometheus.MustRegister(Prometheus.MetricTtlResources)
	prometheus.MustRegister(Prometheus.MetricDeletedResource)
	prometheus.MustRegister(Prometheus.MetricErrors)
}

// start and handle prometheus handler
func startHttpServer() {
	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(opts.ServerBind, nil))
}
