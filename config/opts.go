package config

import (
	"encoding/json"
	"regexp"
	"time"
)

type (
	Opts struct {
		DryRun bool `long:"dry-run"           env:"DRYRUN"  description:"Dry run (no delete)"`

		// logger
		Logger struct {
			Level  string `long:"log.level"    env:"LOG_LEVEL"   description:"Log level" choice:"trace" choice:"debug" choice:"info" choice:"warning" choice:"error" default:"info"`                          // nolint:staticcheck // multiple choices are ok
			Format string `long:"log.format"   env:"LOG_FORMAT"  description:"Log format" choice:"logfmt" choice:"json" default:"logfmt"`                                                                     // nolint:staticcheck // multiple choices are ok
			Source string `long:"log.source"   env:"LOG_SOURCE"  description:"Show source for every log message (useful for debugging and bug reports)" choice:"" choice:"short" choice:"file" choice:"full"` // nolint:staticcheck // multiple choices are ok
			Color  string `long:"log.color"    env:"LOG_COLOR"   description:"Enable color for logs" choice:"" choice:"auto" choice:"yes" choice:"no"`                                                        // nolint:staticcheck // multiple choices are ok
			Time   bool   `long:"log.time"     env:"LOG_TIME"    description:"Show log time"`
		}

		// azure
		Azure struct {
			Environment  *string  `long:"azure.environment"    env:"AZURE_ENVIRONMENT"                     description:"Azure environment name" default:"AZUREPUBLICCLOUD"`
			Subscription []string `long:"azure.subscription"   env:"AZURE_SUBSCRIPTION_ID"  env-delim:" "  description:"Azure subscription ID (space delimiter)"`
			ResourceTags []string `long:"azure.resource-tag"   env:"AZURE_RESOURCE_TAG"     env-delim:" "  description:"Azure Resource tags (space delimiter)"     default:"owner"`
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
				Ttl    time.Duration `long:"janitor.deployments.ttl"     env:"JANITOR_DEPLOYMENTS_TTL"     description:"Janitor deployment ttl (time.duration)"  default:"8760h"`
				Limit  int64         `long:"janitor.deployments.limit"   env:"JANITOR_DEPLOYMENTS_LIMIT"   description:"Janitor deployment limit count (int)"    default:"700"`
			}

			RoleAssignments struct {
				Enable               bool          `long:"janitor.roleassignments"                    env:"JANITOR_ROLEASSIGNMENTS_ENABLE"                          description:"Enable Azure RoleAssignments cleanup"`
				Ttl                  time.Duration `long:"janitor.roleassignments.ttl"                env:"JANITOR_ROLEASSIGNMENTS_TTL"                             description:"Janitor roleassignment ttl (time.duration)"  default:"6h"`
				RoleDefintionIds     []string      `long:"janitor.roleassignments.roledefinitionid"   env:"JANITOR_ROLEASSIGNMENTS_ROLEDEFINITIONID"  env-delim:" " description:"Janitor roledefinition ID (eg: /subscriptions/xxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx/providers/Microsoft.Authorization/roleDefinitions/xxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx or /providers/Microsoft.Authorization/roleDefinitions/xxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx for subscription independent roleDefinitions)  (space delimiter)"`
				AdditionalFilter     *string       `long:"janitor.roleassignments.filter"             env:"JANITOR_ROLEASSIGNMENTS_FILTER"                          description:"Additional $filter for Azure REST API for RoleAssignments"`
				Filter               string
				DescriptionTtl       *string `long:"janitor.roleassignments.descriptionttl"           env:"JANITOR_ROLEASSIGNMENTS_DESCRIPTIONTTL"                  description:"Regexp for detecting ttl inside description of RoleAssignment"`
				DescriptionTtlRegExp *regexp.Regexp
			}
		}

		Server struct {
			// general options
			Bind         string        `long:"server.bind"              env:"SERVER_BIND"           description:"Server address"        default:":8080"`
			ReadTimeout  time.Duration `long:"server.timeout.read"      env:"SERVER_TIMEOUT_READ"   description:"Server read timeout"   default:"5s"`
			WriteTimeout time.Duration `long:"server.timeout.write"     env:"SERVER_TIMEOUT_WRITE"  description:"Server write timeout"  default:"10s"`
		}
	}
)

func (o *Opts) GetJson() []byte {
	jsonBytes, err := json.Marshal(o)
	if err != nil {
		panic(err)
	}
	return jsonBytes
}
