package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strings"

	"github.com/jessevdk/go-flags"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/webdevops/go-common/azuresdk/armclient"
	"github.com/webdevops/go-common/azuresdk/azidentity"
	"github.com/webdevops/go-common/azuresdk/prometheus/tracing"

	"github.com/webdevops/azure-janitor/config"
	"github.com/webdevops/azure-janitor/janitor"
)

const (
	Author = "webdevops.io"

	UserAgent = "azure-janitor/"
)

var (
	argparser *flags.Parser
	Opts      config.Opts

	AzureClient *armclient.ArmClient

	// Git version information
	gitCommit = "<unknown>"
	gitTag    = "<unknown>"
)

func main() {
	initArgparser()

	logger.Infof("starting azure-janitor v%s (%s; %s; by %v)", gitTag, gitCommit, runtime.Version(), Author)
	logger.Info(string(Opts.GetJson()))

	logger.Infof("init Azure connection")
	initAzureConnection()

	logger.Infof("init Janitor")
	j := janitor.Janitor{
		Conf:      Opts,
		UserAgent: UserAgent + gitTag,
		Logger:    logger,
		Azure: janitor.JanitorAzureConfig{
			Client:       AzureClient,
			Subscription: Opts.Azure.Subscription,
		},
	}
	j.Init()
	j.Run()

	logger.Infof("starting http server on %s", Opts.Server.Bind)
	startHttpServer()
}

// init argparser and parse/validate arguments
func initArgparser() {
	argparser = flags.NewParser(&Opts, flags.Default)
	_, err := argparser.Parse()

	// check if there is an parse error
	if err != nil {
		var flagsErr *flags.Error
		if ok := errors.As(err, &flagsErr); ok && flagsErr.Type == flags.ErrHelp {
			os.Exit(0)
		} else {
			fmt.Println()
			argparser.WriteHelp(os.Stdout)
			os.Exit(1)
		}
	}

	initLogger()

	// resourceGroup filter
	Opts.Janitor.ResourceGroups.Filter = fmt.Sprintf(
		"tagName eq '%s'",
		strings.Replace(Opts.Janitor.Tag, "'", "\\'", -1),
	)

	// ResourceGroups: add additional filter
	if Opts.Janitor.ResourceGroups.AdditionalFilter != nil {
		Opts.Janitor.ResourceGroups.Filter = fmt.Sprintf(
			"%s and %s",
			Opts.Janitor.ResourceGroups.Filter,
			*Opts.Janitor.ResourceGroups.AdditionalFilter,
		)
	}

	// Resources: add additional filter
	// resource (if we specify tagValue here we don't get the tag.. wtf?!)
	Opts.Janitor.Resources.Filter = ""
	if Opts.Janitor.Resources.AdditionalFilter != nil {
		Opts.Janitor.Resources.Filter = *Opts.Janitor.Resources.AdditionalFilter
	}

	if Opts.Janitor.RoleAssignments.Enable {
		if len(Opts.Janitor.RoleAssignments.RoleDefintionIds) == 0 {
			logger.Panic("roleAssignment janitor active but no roleDefinitionIds defined")
		}
	}

	// RoleAssignments: add additional filter
	if Opts.Janitor.RoleAssignments.AdditionalFilter != nil {
		Opts.Janitor.RoleAssignments.Filter = *Opts.Janitor.RoleAssignments.AdditionalFilter
	}

	if !Opts.Janitor.ResourceGroups.Enable && !Opts.Janitor.Resources.Enable && !Opts.Janitor.Deployments.Enable && !Opts.Janitor.RoleAssignments.Enable {
		logger.Fatal("no janitor task (resources, resourcegroups, deployments, roleassignments) enabled, not starting")
	}

	if Opts.Janitor.RoleAssignments.DescriptionTtl != nil {
		Opts.Janitor.RoleAssignments.DescriptionTtlRegExp = regexp.MustCompile(*Opts.Janitor.RoleAssignments.DescriptionTtl)
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
			logger.Panicf(`unsupported environment variable "%v" detected: %v`, envVar, solution)
		}
	}
}

func initAzureConnection() {
	var err error

	if Opts.Azure.Environment != nil {
		if err := os.Setenv(azidentity.EnvAzureEnvironment, *Opts.Azure.Environment); err != nil {
			logger.Warnf(`unable to set envvar "%s": %v`, azidentity.EnvAzureEnvironment, err.Error())
		}
	}

	AzureClient, err = armclient.NewArmClientFromEnvironment(logger)
	if err != nil {
		logger.Fatal(err.Error())
	}
	AzureClient.SetUserAgent(UserAgent + gitTag)
}

// start and handle prometheus handler
func startHttpServer() {
	mux := http.NewServeMux()

	// healthz
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if _, err := fmt.Fprint(w, "Ok"); err != nil {
			logger.Error(err)
		}
	})

	// readyz
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if _, err := fmt.Fprint(w, "Ok"); err != nil {
			logger.Error(err)
		}
	})

	mux.Handle("/metrics", tracing.RegisterAzureMetricAutoClean(promhttp.Handler()))

	srv := &http.Server{
		Addr:         Opts.Server.Bind,
		Handler:      mux,
		ReadTimeout:  Opts.Server.ReadTimeout,
		WriteTimeout: Opts.Server.WriteTimeout,
	}
	logger.Fatal(srv.ListenAndServe())
}
