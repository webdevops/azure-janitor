package janitor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/resources"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/subscriptions"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/to"
	tparse "github.com/karrick/tparse/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rickb777/date/period"
	log "github.com/sirupsen/logrus"
	azureCommon "github.com/webdevops/go-common/azure"
	prometheusCommon "github.com/webdevops/go-common/prometheus"

	"github.com/webdevops/azure-janitor/config"
)

const (
	ApiVersionNoLocation = "UNDEFINED"
)

type (
	Janitor struct {
		apiVersionMap map[string]map[string]string

		Conf  config.Opts
		Azure JanitorAzureConfig

		UserAgent string

		Prometheus struct {
			MetricDuration           *prometheus.GaugeVec
			MetricTtlResources       *prometheus.GaugeVec
			MetricTtlRoleAssignments *prometheus.GaugeVec
			MetricDeletedResource    *prometheus.CounterVec
			MetricErrors             *prometheus.CounterVec
		}
	}

	JanitorAzureConfig struct {
		Client       *azureCommon.Client
		Subscription []string
	}
)

var (
	janitorTimeFormats = []string{
		// preferred format
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

		// least preferred format
		"2006-01-02",
	}
)

func (j *Janitor) Init() {
	j.initPrometheus()
	j.initAuzreApiVersions()
}

func (j *Janitor) initPrometheus() {
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
		azureCommon.AddResourceTagsToPrometheusLabelsDefinition(
			[]string{
				"resourceID",
				"subscriptionID",
				"resourceGroup",
				"resourceType",
			},
			j.Conf.Azure.ResourceTags,
		),
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
}

func (j *Janitor) subscriptionList(ctx context.Context) []subscriptions.Subscription {
	subscriptionList, err := j.Azure.Client.ListCachedSubscriptionsWithFilter(ctx, j.Azure.Subscription...)
	if err != nil {
		log.Panic(err.Error())
	}
	return subscriptionList
}

func (j *Janitor) Run() {
	ctx := context.Background()

	go func() {
		for {
			runLogger := log.WithFields(log.Fields{})

			startTime := time.Now()
			runLogger.Infof("start janitor run")
			var wgMain sync.WaitGroup
			var wgMetrics sync.WaitGroup

			callbackTtlResourcesMetrics := make(chan *prometheusCommon.MetricList)
			callbackTtlRoleAssignmentsMetrics := make(chan *prometheusCommon.MetricList)

			// subscription processing
			go func() {
				for _, subscription := range j.subscriptionList(ctx) {
					wgMain.Add(1)
					go func(subscription subscriptions.Subscription) {
						defer wgMain.Done()

						contextLogger := runLogger.WithFields(log.Fields{
							"subscriptionID":   to.String(subscription.SubscriptionID),
							"subscriptionName": to.String(subscription.DisplayName),
						})

						if j.Conf.Janitor.Deployments.Enable {
							j.runDeployments(ctx, contextLogger, subscription, callbackTtlResourcesMetrics)
						}

						if j.Conf.Janitor.Resources.Enable {
							j.runResources(ctx, contextLogger, subscription, j.Conf.Janitor.Resources.Filter, callbackTtlResourcesMetrics)
						}

						if j.Conf.Janitor.RoleAssignments.Enable {
							j.runRoleAssignments(ctx, contextLogger, subscription, j.Conf.Janitor.RoleAssignments.Filter, callbackTtlRoleAssignmentsMetrics)
						}

						if j.Conf.Janitor.ResourceGroups.Enable {
							j.runResourceGroups(ctx, contextLogger, subscription, j.Conf.Janitor.ResourceGroups.Filter, callbackTtlResourcesMetrics)
						}
					}(subscription)
				}
				wgMain.Wait()
				close(callbackTtlResourcesMetrics)
				close(callbackTtlRoleAssignmentsMetrics)
			}()

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

			// wait for metrics processing
			wgMetrics.Wait()

			duration := time.Since(startTime)
			j.Prometheus.MetricDuration.With(prometheus.Labels{}).Set(duration.Seconds())

			runLogger.WithField("duration", duration.Seconds()).Infof("finished run in %s, waiting %s", duration.String(), j.Conf.Janitor.Interval.String())
			time.Sleep(j.Conf.Janitor.Interval)
		}
	}()
}

func (j *Janitor) initAuzreApiVersions() {
	ctx := context.Background()

	j.apiVersionMap = map[string]map[string]string{}
	for _, subscription := range j.subscriptionList(ctx) {
		subscriptionId := to.String(subscription.SubscriptionID)

		contextLogger := log.WithFields(log.Fields{
			"subscriptionID":   to.String(subscription.SubscriptionID),
			"subscriptionName": to.String(subscription.DisplayName),
		})

		// fetch location translation map
		locationClient := subscriptions.NewClientWithBaseURI(j.Azure.Client.Environment.ResourceManagerEndpoint)
		j.decorateAzureAutorest(&locationClient.Client)

		locationResult, err := locationClient.ListLocations(ctx, subscriptionId, nil)
		if err != nil {
			contextLogger.Panic(err.Error())
		}

		locationMap := map[string]string{}
		for _, location := range *locationResult.Value {
			locationDisplayName := to.String(location.DisplayName)
			locationName := to.String(location.Name)
			locationMap[locationDisplayName] = locationName
		}

		// fetch providers
		providersClient := resources.NewProvidersClientWithBaseURI(j.Azure.Client.Environment.ResourceManagerEndpoint, *subscription.SubscriptionID)
		j.decorateAzureAutorest(&providersClient.Client)

		result, err := providersClient.ListComplete(ctx, nil, "")
		if err != nil {
			contextLogger.Panic(err.Error())
		}

		j.apiVersionMap[subscriptionId] = map[string]string{}
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

				// select best last apiversion
				lastApiVersion := ""
				lastApiPreviewVersion := ""
				providerApiVersion := ""
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

				// choose best apiversion
				if lastApiVersion != "" {
					providerApiVersion = lastApiVersion
				} else if lastApiPreviewVersion != "" {
					providerApiVersion = lastApiPreviewVersion
				}

				// add all locations (if available)
				for _, location := range *resourceType.Locations {
					// try to translate location to internal type
					if val, ok := locationMap[location]; ok {
						location = val
					}

					key := strings.ToLower(fmt.Sprintf("%s::%s", location, resourceTypeName))
					j.apiVersionMap[subscriptionId][key] = providerApiVersion
				}

				// add no location fallback
				key := strings.ToLower(fmt.Sprintf("%s::%s", ApiVersionNoLocation, resourceTypeName))
				j.apiVersionMap[subscriptionId][key] = providerApiVersion

			}
		}
	}
}

func (j *Janitor) getAzureApiVersionForResourceType(subscriptionId, location, resourceType string) (apiVersion string) {
	locationKey := strings.ToLower(fmt.Sprintf("%s::%s", location, resourceType))
	unknownKey := strings.ToLower(fmt.Sprintf("%s::%s", ApiVersionNoLocation, resourceType))
	if val, ok := j.apiVersionMap[subscriptionId][locationKey]; ok {
		// location based apiVersion
		apiVersion = val
	} else if val, ok := j.apiVersionMap[subscriptionId][unknownKey]; ok {
		// unknown location based apiVersion
		apiVersion = val
	}
	return
}

func (j *Janitor) checkAzureResourceExpiry(logger *log.Entry, resourceType, resourceId string, resourceTags *map[string]*string) (resourceExpireTime *time.Time, resourceExpired bool, resourceTagRewriteNeeded bool) {
	ttlValue := j.getTtlTagFromAzureResource(*resourceTags)

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
		} else if val, durationParseErr := j.parseExpiryAndBuildExpiryTime(*ttlValue); durationParseErr == nil && val != nil {
			// try parse as duration
			logger.Infof("found valid duration (%v)", *ttlValue)
			ttlValue := val.Format(time.RFC3339)
			(*resourceTags)[j.Conf.Janitor.TagTarget] = &ttlValue

			resourceTagRewriteNeeded = true
			resourceExpireTime = val
		} else {
			logger.Errorf("unable to parse time: %v", timeParseErr.Error())
		}
	}

	return
}

func (j *Janitor) getTtlTagFromAzureResource(tags map[string]*string) *string {
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

func (j *Janitor) parseExpiryDuration(value string) (duration *time.Duration, err error) {
	// sanity checks
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}

	now := time.Now()

	// ISO8601 style duration
	if val, parseErr := period.Parse(value); parseErr == nil {
		// parse duration
		dur, _ := val.Duration()
		return &dur, nil
	}

	// golang style duration
	if val, parseErr := tparse.AddDuration(now, value); parseErr == nil {
		dur := val.Sub(now)
		return &dur, nil
	}

	return nil, fmt.Errorf("unable to parse '%v' as duration", value)
}

func (j *Janitor) parseExpiryAndBuildExpiryTime(value string) (parsedTime *time.Time, err error) {
	if duration, err := j.parseExpiryDuration(value); err == nil {
		expiryTime := time.Now().Add(*duration)
		return &expiryTime, nil
	} else {
		return nil, err
	}
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

func (j *Janitor) decorateAzureAutorest(client *autorest.Client) {
	j.Azure.Client.DecorateAzureAutorest(client)
}
