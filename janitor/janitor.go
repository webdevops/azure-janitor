package janitor

import (
	"context"
	"fmt"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/features"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/subscriptions"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure"
	tparse "github.com/karrick/tparse/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rickb777/date/period"
	log "github.com/sirupsen/logrus"
	"github.com/webdevops/azure-janitor/config"
	prometheusCommon "github.com/webdevops/go-prometheus-common"
	"strings"
	"sync"
	"time"
)

type (
	Janitor struct {
		apiVersionMap map[string]map[string]string

		Conf  config.Opts
		Azure JanitorAzureConfig

		Prometheus struct {
			MetricDuration           *prometheus.GaugeVec
			MetricTtlResources       *prometheus.GaugeVec
			MetricTtlRoleAssignments *prometheus.GaugeVec
			MetricDeletedResource    *prometheus.CounterVec
			MetricErrors             *prometheus.CounterVec
		}
	}

	JanitorAzureConfig struct {
		Authorizer    autorest.Authorizer
		Subscriptions []subscriptions.Subscription
		Environment   azure.Environment
	}
)

var (
	janitorTimeFormats = []string{
		// prefered format
		time.RFC3339,

		// human format
		"2006-01-02 15:04:05 +07:00",
		"2006-01-02 15:04:05 MST",
		"2006-01-02 15:04:05",

		// allowed formats
		time.RFC822,
		time.RFC822Z,
		time.RFC850,
		time.RFC1123,
		time.RFC1123Z,
		time.RFC3339Nano,
	}
)

func (j *Janitor) Init() {
	j.Prometheus.MetricDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "azurejanitor_duration",
			Help: "AzureJanitor cleanup duration",
		},
		[]string{},
	)
	prometheus.MustRegister(j.Prometheus.MetricDuration)

	j.Prometheus.MetricTtlResources = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "azurejanitor_resources_ttl",
			Help: "AzureJanitor resources with expiry time",
		},
		[]string{
			"resourceID",
			"subscriptionID",
			"resourceGroup",
			"provider",
		},
	)
	prometheus.MustRegister(j.Prometheus.MetricTtlResources)

	j.Prometheus.MetricTtlRoleAssignments = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "azurejanitor_roleassignment_ttl",
			Help: "AzureJanitor roleassignments with expiry time",
		},
		[]string{
			"roleAssignmentId",
			"scope",
			"principalId",
			"principalType",
			"roleDefinitionId",
			"subscriptionID",
			"resourceGroup",
		},
	)
	prometheus.MustRegister(j.Prometheus.MetricTtlRoleAssignments)

	j.Prometheus.MetricDeletedResource = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "azurejanitor_resources_deleted",
			Help: "AzureJanitor deleted resources",
		},
		[]string{
			"subscriptionID",
			"resourceType",
		},
	)
	prometheus.MustRegister(j.Prometheus.MetricDeletedResource)

	j.Prometheus.MetricErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "azurejanitor_errors",
			Help: "AzureJanitor error counter",
		},
		[]string{
			"subscriptionID",
			"resourceType",
		},
	)
	prometheus.MustRegister(j.Prometheus.MetricErrors)

	j.initAuzreApiVersions()
}

func (j *Janitor) Run() {
	ctx := context.Background()

	go func() {
		for {
			startTime := time.Now()
			log.Infof("start janitor run")
			var wgMain sync.WaitGroup
			var wgMetrics sync.WaitGroup

			callbackTtlResourcesMetrics := make(chan *prometheusCommon.MetricList)
			callbackTtlRoleAssignmentsMetrics := make(chan *prometheusCommon.MetricList)

			// subscription processing
			for _, subscription := range j.Azure.Subscriptions {
				wgMain.Add(1)
				go func(subscription subscriptions.Subscription) {
					defer wgMain.Done()

					if j.Conf.Janitor.ResourceGroups.Enable {
						j.runResourceGroups(ctx, subscription, j.Conf.Janitor.ResourceGroups.Filter, callbackTtlResourcesMetrics)
					}

					if j.Conf.Janitor.Resources.Enable {
						j.runResources(ctx, subscription, j.Conf.Janitor.Resources.Filter, callbackTtlResourcesMetrics)
					}

					if j.Conf.Janitor.Deployments.Enable {
						j.runDeployments(ctx, subscription, callbackTtlResourcesMetrics)
					}

					if j.Conf.Janitor.RoleAssignments.Enable {
						j.runRoleAssignments(ctx, subscription, j.Conf.Janitor.RoleAssignments.Filter, callbackTtlRoleAssignmentsMetrics)
					}
				}(subscription)
			}

			// channel collecting gofunc
			wgMetrics.Add(1)
			go func() {
				defer wgMetrics.Done()

				// store metriclists from channel
				ttlMetricListList := []prometheusCommon.MetricList{}
				for ttlMetrics := range callbackTtlRoleAssignmentsMetrics {
					if ttlMetrics != nil {
						ttlMetricListList = append(ttlMetricListList, *ttlMetrics)
					}
				}

				// after channel is closed: reset metric and set them to the new state
				j.Prometheus.MetricTtlRoleAssignments.Reset()
				for _, ttlMetrics := range ttlMetricListList {
					ttlMetrics.GaugeSet(j.Prometheus.MetricTtlRoleAssignments)
				}
			}()

			wgMetrics.Add(1)
			go func() {
				defer wgMetrics.Done()

				// store metriclists from channel
				ttlMetricListList := []prometheusCommon.MetricList{}
				for ttlMetrics := range callbackTtlResourcesMetrics {
					if ttlMetrics != nil {
						ttlMetricListList = append(ttlMetricListList, *ttlMetrics)
					}
				}

				// after channel is closed: reset metric and set them to the new state
				j.Prometheus.MetricTtlResources.Reset()
				for _, ttlMetrics := range ttlMetricListList {
					ttlMetrics.GaugeSet(j.Prometheus.MetricTtlResources)
				}
			}()

			// wait for subscription main func, then close channel and wait for metrics
			wgMain.Wait()
			close(callbackTtlResourcesMetrics)
			close(callbackTtlRoleAssignmentsMetrics)
			wgMetrics.Wait()

			duration := time.Since(startTime)
			j.Prometheus.MetricDuration.With(prometheus.Labels{}).Set(duration.Seconds())

			log.WithField("duration", duration.Seconds()).Infof("finished run in %s, waiting %s", duration.String(), j.Conf.Janitor.Interval.String())
			time.Sleep(j.Conf.Janitor.Interval)
		}
	}()
}

func (j *Janitor) initAuzreApiVersions() {
	ctx := context.Background()

	j.apiVersionMap = map[string]map[string]string{}
	for _, subscription := range j.Azure.Subscriptions {
		client := features.NewProvidersClient(*subscription.SubscriptionID)
		client.Authorizer = j.Azure.Authorizer

		subscriptionId := *subscription.SubscriptionID

		j.apiVersionMap[subscriptionId] = map[string]string{}

		result, err := client.ListComplete(ctx, nil, "")
		if err != nil {
			panic(err)
		}

		for _, provider := range *result.Response().Value {
			if provider.ResourceTypes == nil {
				continue
			}

			for _, resourceType := range *provider.ResourceTypes {
				if resourceType.APIVersions == nil {
					continue
				}

				resourceTypeName := fmt.Sprintf(
					"%s/%s",
					strings.ToLower(*provider.Namespace),
					strings.ToLower(*resourceType.ResourceType),
				)

				lastApiVersion := ""
				lastApiPreviewVersion := ""
				for _, apiVersion := range *resourceType.APIVersions {
					if strings.Contains(apiVersion, "-preview") {
						if lastApiVersion == "" || lastApiPreviewVersion > apiVersion {
							lastApiPreviewVersion = apiVersion
						}
					} else {
						if lastApiVersion == "" || lastApiVersion > apiVersion {
							lastApiVersion = apiVersion
						}
					}
				}

				if lastApiVersion != "" {
					j.apiVersionMap[subscriptionId][resourceTypeName] = lastApiVersion
				} else if lastApiPreviewVersion != "" {
					j.apiVersionMap[subscriptionId][resourceTypeName] = lastApiPreviewVersion
				}
			}
		}
	}
}

func (j *Janitor) getAzureApiVersionForSubscriptionResourceType(subscriptionId, resourceType string) (apiVersion string) {
	resourceType = strings.ToLower(resourceType)
	if val, ok := j.apiVersionMap[subscriptionId][resourceType]; ok {
		apiVersion = val
	}
	return
}

func (j *Janitor) checkAzureResourceExpiry(logger *log.Entry, resourceType, resourceId string, resourceTags *map[string]*string) (resourceExpireTime *time.Time, resourceExpired bool, resourceTagRewriteNeeded bool) {
	ttlValue := j.getTtlTagFromAzureResoruce(*resourceTags)

	if ttlValue != nil {
		if j.Conf.Logger.Verbose {
			logger.Infof("checking ttl")
		}

		tagValueParsed, tagValueExpired, timeParseErr := j.checkExpiryDate(*ttlValue)
		if timeParseErr == nil {
			// date parsed successfully
			if tagValueExpired {
				if j.Conf.DryRun {
					logger.Infof("expired, but dryrun active")
				} else {
					resourceExpired = true
				}
			} else {
				if j.Conf.Logger.Verbose {
					logger.Infof("NOT expired")
				}
			}

			resourceExpireTime = tagValueParsed
		} else if val, durationParseErr := j.checkExpiryDuration(*ttlValue); durationParseErr == nil && val != nil {
			// try parse as duration
			logger.Infof("found valid duration (%v)", *ttlValue)
			ttlValue := val.Format(time.RFC3339)
			(*resourceTags)[j.Conf.Janitor.TagTarget] = &ttlValue

			resourceTagRewriteNeeded = true
			resourceExpireTime = val
		} else {
			logger.Errorf("ERROR %s", timeParseErr)
		}
	}

	return
}

func (j *Janitor) getTtlTagFromAzureResoruce(tags map[string]*string) *string {
	// check target tag first
	for tagName, tagValue := range tags {
		if tagName == j.Conf.Janitor.TagTarget && tagValue != nil && *tagValue != "" {
			return tagValue
		}
	}

	// check source tag last
	for tagName, tagValue := range tags {
		if tagName == j.Conf.Janitor.Tag && tagValue != nil && *tagValue != "" {
			return tagValue
		}
	}

	return nil
}

func (j *Janitor) checkExpiryDuration(value string) (parsedTime *time.Time, err error) {
	// sanity checks
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}

	// ISO8601 style duration
	if val, parseErr := period.Parse(value); parseErr == nil {
		// parse duration
		calcTime := time.Now().Add(val.DurationApprox())
		parsedTime = &calcTime
		return
	}

	// golang style duration
	if val, parseErr := tparse.AddDuration(time.Now(), value); parseErr == nil {
		parsedTime = &val
		return
	}

	err = fmt.Errorf("unable to parse '%v' as duration", value)

	return
}

func (j *Janitor) checkExpiryDate(value string) (parsedTime *time.Time, expired bool, err error) {
	expired = false

	// sanity checks
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}

	// parse time
	for _, timeFormat := range janitorTimeFormats {
		if parseVal, parseErr := time.Parse(timeFormat, value); parseErr == nil && parseVal.Unix() > 0 {
			parsedTime = &parseVal
			break
		}
	}

	// check if time could be parsed
	if parsedTime != nil {
		// check if parsed time is before NOW -> expired
		expired = parsedTime.Before(time.Now())
	} else {
		err = fmt.Errorf("unable to parse time '%s'", value)
	}

	return
}
