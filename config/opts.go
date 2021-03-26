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
			Environment  *string  `long:"azure-environment"    env:"AZURE_ENVIRONMENT"                description:"Azure environment name" default:"AZUREPUBLICCLOUD"`
			Subscription []string `long:"azure-subscription"   env:"AZURE_SUBSCRIPTION_ID"  env-delim:" "  description:"Azure subscription ID"`
		}

		// janitor
		Janitor struct {
			// Janitor settings
			Interval  time.Duration `long:"janitor.interval"    env:"JANITOR_INTERVAL"    description:"Janitor interval (time.duration)"  default:"1h"`
			Tag       string        `long:"janitor.tag"         env:"JANITOR_TAG"         description:"Janitor azure tag (string)"  default:"ttl"`
			TagTarget string        `long:"janitor.tag.target"  env:"JANITOR_TAG_TARGET"  description:"Janitor azure tag (string)"  default:"ttl_expiry"`

			ResourceGroups struct {
				Enable           bool    `long:"janitor.resourcegroups"         env:"JANITOR_RESOURCEGROUPS_ENABLE"  description:"Enable Azure ResourceGroups cleanup"`
				AdditionalFilter *string `long:"janitor.resourcegroups.filter"  env:"JANITOR_RESOURCEGROUPS_FILTER"  description:"Additional $filter for Azure REST API for ResourceGroups"`
				Filter           string
			}

			Resources struct {
				Enable           bool    `long:"janitor.resources"          env:"JANITOR_RESOURCES_ENABLE"  description:"Enable Azure Resources cleanup"`
				AdditionalFilter *string `long:"janitor.resources.filter"   env:"JANITOR_RESOURCES_FILTER"  description:"Additional $filter for Azure REST API for Resources"`
				Filter           string
			}

			Deployments struct {
				Enable bool          `long:"janitor.deployments"         env:"JANITOR_DEPLOYMENTS_ENABLE"  description:"Enable Azure Deployments cleanup"`
				Ttl    time.Duration `long:"janitor.deployments.ttl"     env:"JANITOR_DEPLOYMENTS_TTL"  description:"Janitor deployment ttl (time.duration)"  default:"8760h"`
				Limit  int64         `long:"janitor.deployments.limit"   env:"JANITOR_DEPLOYMENTS_LIMIT"  description:"Janitor deployment limit count (int)"  default:"700"`
			}

			RoleAssignments struct {
				Enable           bool          `long:"janitor.roleassignments"                    env:"JANITOR_ROLEASSIGNMENTS_ENABLE"  description:"Enable Azure RoleAssignments cleanup"`
				Ttl              time.Duration `long:"janitor.roleassignments.ttl"                env:"JANITOR_ROLEASSIGNMENTS_TTL"  description:"Janitor roleassignment ttl (time.duration)"  default:"6h"`
				RoleDefintionIds []string      `long:"janitor.roleassignments.roledefinitionid"   env:"JANITOR_ROLEASSIGNMENTS_ROLEDEFINITIONID"  env-delim:" " description:"Janitor roledefinition ID (eg: /subscriptions/xxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx/providers/Microsoft.Authorization/roleDefinitions/xxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx)"`
				AdditionalFilter *string       `long:"janitor.roleassignments.filter"             env:"JANITOR_ROLEASSIGNMENTS_FILTER"  description:"Additional $filter for Azure REST API for RoleAssignments"`
				Filter           string
			}
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
