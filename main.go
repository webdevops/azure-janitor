package main

import (
	"context"
	"fmt"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/subscriptions"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/jessevdk/go-flags"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"github.com/webdevops/azure-janitor/config"
	"github.com/webdevops/azure-janitor/janitor"
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
)

func main() {
	initArgparser()

	log.Infof("starting azure-janitor v%s (%s; %s; by %v)", gitTag, gitCommit, runtime.Version(), Author)
	log.Info(string(opts.GetJson()))

	log.Infof("init Azure connection")
	initAzureConnection()

	log.Infof("init Janitor")
	j := janitor.Janitor{
		Conf: opts,
		Azure: janitor.JanitorAzureConfig{
			Authorizer:    azureAuthorizer,
			Subscriptions: azureSubscriptions,
			Environment:   azureEnvironment,
		},
	}
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
	opts.Janitor.ResourceGroups.Filter = fmt.Sprintf(
		"tagName eq '%s'",
		strings.Replace(opts.Janitor.Tag, "'", "\\'", -1),
	)

	// ResourceGroups: add additional filter
	if opts.Janitor.ResourceGroups.AdditionalFilter != nil {
		opts.Janitor.ResourceGroups.Filter = fmt.Sprintf(
			"%s and %s",
			opts.Janitor.ResourceGroups.Filter,
			*opts.Janitor.ResourceGroups.AdditionalFilter,
		)
	}

	// Resources: add additional filter
	// resource (if we specify tagValue here we don't get the tag.. wtf?!)
	opts.Janitor.Resources.Filter = ""
	if opts.Janitor.Resources.AdditionalFilter != nil {
		opts.Janitor.Resources.Filter = *opts.Janitor.Resources.AdditionalFilter
	}

	if opts.Janitor.RoleAssignments.Enable {
		if len(opts.Janitor.RoleAssignments.RoleDefintionIds) == 0 {
			log.Panic("roleAssignment janitor active but no roleDefinitionIds defined")
		}
	}

	// RoleAssignments: add additional filter
	if opts.Janitor.RoleAssignments.AdditionalFilter != nil {
		opts.Janitor.RoleAssignments.Filter = *opts.Janitor.RoleAssignments.AdditionalFilter
	}

	if !opts.Janitor.ResourceGroups.Enable && !opts.Janitor.Resources.Enable && !opts.Janitor.Deployments.Enable && !opts.Janitor.RoleAssignments.Enable {
		log.Fatal("no janitor task (resources, resourcegroups, deployments, roleassignments) enabled, not starting")
	}

	checkForDeprecations()
}

func checkForDeprecations() {
	deprecatedEnvVars := map[string]string{
		`JANITOR_ENABLE_DEPLOYMENTS`: `use env "JANITOR_DEPLOYMENTS_ENABLE" instead`,
		`JANITOR_DEPLOYMENT_TTL`:     `use env "JANITOR_DEPLOYMENTS_TTL" instead`,
		`JANITOR_DEPLOYMENT_LIMIT`:   `use env "JANITOR_DEPLOYMENTS_LIMIT" instead`,

		`JANITOR_ENABLE_RESOURCEGROUPS`: `use env "JANITOR_RESOURCEGROUPS_ENABLE" instead`,
		`JANITOR_FILTER_RESOURCEGROUPS`: `use env "JANITOR_RESOURCEGROUPS_FILTER" instead`,

		`JANITOR_ENABLE_RESOURCES`: `use env "JANITOR_RESOURCES_ENABLE" instead`,
		`JANITOR_FILTER_RESOURCES`: `use env "JANITOR_RESOURCES_FILTER" instead`,
	}

	for envVar, solution := range deprecatedEnvVars {
		if val := os.Getenv(envVar); val != "" {
			log.Panicf(`unsupported environment variable "%v" detected: %v`, envVar, solution)
		}
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

// start and handle prometheus handler
func startHttpServer() {
	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(opts.ServerBind, nil))
}
