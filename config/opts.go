package config

import (
	"encoding/json"
	log "github.com/sirupsen/logrus"
	"time"
)

type (
	Opts struct {
		DryRun bool `long:"dry-run"           env:"DRYRUN"  description:"Dry run (no delete)"`

		// logger
		Logger struct {
			Debug   bool `           long:"debug"        env:"DEBUG"    description:"debug mode"`
			Verbose bool `short:"v"  long:"verbose"      env:"VERBOSE"  description:"verbose mode"`
			LogJson bool `           long:"log.json"     env:"LOG_JSON" description:"Switch log output to json format"`
		}

		// azure
		Azure struct {
			Environment  *string  `long:"azure-environment"            env:"AZURE_ENVIRONMENT"                description:"Azure environment name" default:"AZUREPUBLICCLOUD"`
			Subscription []string `long:"azure-subscription"            env:"AZURE_SUBSCRIPTION_ID"     env-delim:" "  description:"Azure subscription ID"`
		}

		// janitor
		Janitor struct {
			// Janitor settings
			Interval time.Duration `long:"janitor.interval" env:"JANITOR_INTERVAL"  description:"Janitor interval (time.duration)"  default:"1h"`
			Tag      string        `long:"janitor.tag"      env:"JANITOR_TAG"  description:"Janitor azure tag (string)"  default:"ttl"`

			DeploymentsTtl   time.Duration `long:"janitor.deployment.ttl"      env:"JANITOR_DEPLOYMENT_TTL"  description:"Janitor deploument ttl (time.duration)"  default:"8760h"`
			DeploymentsLimit int64         `long:"janitor.deployment.limit"      env:"JANITOR_DEPLOYMENT_LIMIT"  description:"Janitor deploument limit count (int)"  default:"700"`

			DisableResourceGroups bool `long:"janitor.disable.resourcegroups" env:"JANITOR_DISABLE_RESOURCEGROUPS"  description:"Disable Azure ResourceGroups cleanup"`
			DisableResources      bool `long:"janitor.disable.resources"      env:"JANITOR_DISABLE_RESOURCES"  description:"Disable Azure Resources cleanup"`
			DisableDeployments    bool `long:"janitor.disable.deployments"      env:"JANITOR_DISABLE_DEPLOYMENTS"  description:"Disable Azure Deployments cleanup"`

			AdditionalFilterResourceGroups *string `long:"janitor.filter.resourcegroups" env:"JANITOR_FILTER_RESOURCEGROUPS"  description:"Additional $filter for Azure REST API for ResourceGroups"`
			AdditionalFilterResources      *string `long:"janitor.filter.resources"      env:"JANITOR_FILTER_RESOURCES"  description:"Additional $filter for Azure REST API for Resources"`
			FilterResourceGroups           string
			FilterResources                string
		}

		// general options
		ServerBind string `long:"bind"     env:"SERVER_BIND"   description:"Server address"     default:":8080"`
	}
)

func (o *Opts) GetJson() []byte {
	jsonBytes, err := json.Marshal(o)
	if err != nil {
		log.Panic(err)
	}
	return jsonBytes
}
