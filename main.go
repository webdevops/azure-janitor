package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/subscriptions"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/jessevdk/go-flags"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	Author  = "webdevops.io"
	Version = "0.1.0"
)

var (
	argparser          *flags.Parser
	args               []string
	Verbose            bool
	Logger             *DaemonLogger
	AzureAuthorizer    autorest.Authorizer
	AzureSubscriptions []subscriptions.Subscription

	Prometheus struct {
		MetricTtlResources    *prometheus.GaugeVec
		MetricDeletedResource *prometheus.CounterVec
		MetricErrors          *prometheus.CounterVec
	}
)

var opts struct {
	// general settings
	Verbose []bool `long:"verbose" short:"v" env:"VERBOSE" description:"Verbose mode"`
	DryRun  bool   `long:"dry-run"           env:"DRYRUN"  description:"Dry run (no delete)"`

	// server settings
	ServerBind string `long:"bind" env:"SERVER_BIND" description:"Server address" default:":8080"`

	// azure settings
	AzureSubscription []string `long:"azure.subscription" env:"AZURE_SUBSCRIPTION_ID" env-delim:" "  description:"Azure subscription ID"`
	azureEnvironment  azure.Environment

	// Janitor settings
	JanitorInterval time.Duration `long:"janitor.interval" env:"JANITOR_INTERVAL"  description:"Janitor interval (time.duration)"  default:"1h"`
	JanitorTag      string        `long:"janitor.tag"      env:"JANITOR_TAG"  description:"Janitor azure tag (time.duration)"  default:"ttl"`

	JanitorDisableResourceGroups bool `long:"janitor.disable.resourcegroups" env:"JANITOR_DISABLE_RESOURCEGROUPS"  description:"Disable Azure ResourceGroups cleanup"`
	JanitorDisableResources      bool `long:"janitor.disable.resources"      env:"JANITOR_DISABLE_RESOURCES"  description:"Disable Azure Resources cleanup"`

	JanitorAdditionalFilterResourceGroups *string `long:"janitor.filter.resourcegroups" env:"JANITOR_FILTER_RESOURCEGROUPS"  description:"Additional $filter for Azure REST API for ResourceGroups"`
	JanitorAdditionalFilterResources      *string `long:"janitor.filter.resources"      env:"JANITOR_FILTER_RESOURCES"  description:"Additional $filter for Azure REST API for Resources"`
	janitorFilterResourceGroups           string
	janitorFilterResources                string
}

func main() {
	initArgparser()

	// set verbosity
	Verbose = len(opts.Verbose) >= 1

	Logger = NewLogger(log.Lshortfile, Verbose)
	defer Logger.Close()

	// set verbosity
	Verbose = len(opts.Verbose) >= 1

	Logger.Infof("Init Azure Janitor exporter v%s (written by %v)", Version, Author)

	Logger.Infof("Init Azure connection")
	initAzureConnection()
	initMetricCollector()

	Logger.Infof("Init Janitor")
	Logger.Infof("  interval: %s", opts.JanitorInterval.String())
	Logger.Infof("  tag: %s", opts.JanitorTag)

	if !opts.JanitorDisableResourceGroups {
		Logger.Infof("  enabled Azure ResourceGroups cleanup")
		Logger.Infof("    filter: %s", opts.janitorFilterResourceGroups)
	} else {
		Logger.Infof("  disabled Azure ResourceGroups cleanup")
	}

	if !opts.JanitorDisableResources {
		Logger.Infof("  enabled Azure Resources cleanup")
		Logger.Infof("    filter: %s", opts.janitorFilterResources)
	} else {
		Logger.Infof("  disabled Azure Resources cleanup")
	}

	startAzureJanitor()

	Logger.Infof("Starting http server on %s", opts.ServerBind)
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
			fmt.Println(err)
			fmt.Println()
			argparser.WriteHelp(os.Stdout)
			os.Exit(1)
		}
	}

	// resourceGroup filter
	opts.janitorFilterResourceGroups = fmt.Sprintf(
		"tagName eq '%s'",
		strings.Replace(opts.JanitorTag, "'", "\\'", -1),
	)

	// add additional filter
	if opts.JanitorAdditionalFilterResourceGroups != nil {
		opts.janitorFilterResourceGroups = fmt.Sprintf(
			"%s and %s",
			opts.janitorFilterResourceGroups,
			*opts.JanitorAdditionalFilterResourceGroups,
		)
	}

	// resource (if we specify tagValue here we don't get the tag.. wtf?!)
	opts.janitorFilterResources = ""

	// add additional filter
	if opts.JanitorAdditionalFilterResources != nil {
		opts.janitorFilterResources = *opts.JanitorAdditionalFilterResources
	}
}

// Init and build Azure authorzier
func initAzureConnection() {
	var err error
	ctx := context.Background()

	// setup azure authorizer
	AzureAuthorizer, err = auth.NewAuthorizerFromEnvironment()
	if err != nil {
		panic(err)
	}
	subscriptionsClient := subscriptions.NewClient()
	subscriptionsClient.Authorizer = AzureAuthorizer

	if len(opts.AzureSubscription) == 0 {
		// auto lookup subscriptions
		listResult, err := subscriptionsClient.List(ctx)
		if err != nil {
			panic(err)
		}
		AzureSubscriptions = listResult.Values()

		if len(AzureSubscriptions) == 0 {
			panic(errors.New("No Azure Subscriptions found via auto detection, does this ServicePrincipal have read permissions to the subcriptions?"))
		}
	} else {
		// fixed subscription list
		AzureSubscriptions = []subscriptions.Subscription{}
		for _, subId := range opts.AzureSubscription {
			result, err := subscriptionsClient.Get(ctx, subId)
			if err != nil {
				panic(err)
			}
			AzureSubscriptions = append(AzureSubscriptions, result)
		}
	}

	// try to get cloud name, defaults to public cloud name
	azureEnvName := azure.PublicCloud.Name
	if env := os.Getenv("AZURE_ENVIRONMENT"); env != "" {
		azureEnvName = env
	}

	opts.azureEnvironment, err = azure.EnvironmentFromName(azureEnvName)
	if err != nil {
		panic(err)
	}
}

func initMetricCollector() {

	Prometheus.MetricTtlResources = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "azurejanitor_resources_ttl",
			Help: "AzureJanitor number of resources with TTL",
		},
		append(
			[]string{
				"resourceID",
				"subscriptionID",
				"resourceGroup",
				"provider",
			},
		),
	)

	Prometheus.MetricDeletedResource = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "azurejanitor_resources_deleted",
			Help: "AzureJanitor deleted resources",
		},
		append(
			[]string{
				"resourceType",
			},
		),
	)
	Prometheus.MetricErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "azurejanitor_errors",
			Help: "AzureJanitor error counter",
		},
		append(
			[]string{
				"resourceType",
			},
		),
	)

	prometheus.MustRegister(Prometheus.MetricTtlResources)
	prometheus.MustRegister(Prometheus.MetricDeletedResource)
	prometheus.MustRegister(Prometheus.MetricErrors)
}

// start and handle prometheus handler
func startHttpServer() {
	http.Handle("/metrics", promhttp.Handler())
	Logger.Fatal(http.ListenAndServe(opts.ServerBind, nil))
}
