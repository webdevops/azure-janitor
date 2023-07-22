package janitor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armsubscriptions"
	tparse "github.com/karrick/tparse/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rickb777/date/period"
	"github.com/webdevops/go-common/azuresdk/armclient"
	"github.com/webdevops/go-common/utils/to"
	"go.uber.org/zap"

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

		Logger *zap.SugaredLogger

		UserAgent string

		Prometheus struct {
			MetricDuration           *prometheus.GaugeVec
			MetricDeployment         *prometheus.GaugeVec
			MetricTtlResources       *prometheus.GaugeVec
			MetricTtlRoleAssignments *prometheus.GaugeVec
			MetricDeletedResource    *prometheus.CounterVec
			MetricErrors             *prometheus.CounterVec
		}
	}

	JanitorAzureConfig struct {
		Client                *armclient.ArmClient
		Subscription          []string
		SubscriptionsIterator *armclient.SubscriptionsIterator
		ResourceTagManager    *armclient.ResourceTagManager
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
	// init subscription iterator
	j.Azure.SubscriptionsIterator = armclient.NewSubscriptionIterator(j.Azure.Client, j.Conf.Azure.Subscription...)

	j.initPrometheus()
	j.initAuzreApiVersions()
}

func (j *Janitor) Run() {
	ctx := context.Background()

	go func() {
		for {
			runLogger := j.Logger

			startTime := time.Now()
			runLogger.Infof("start janitor run")

			callbackFuncs := make(chan func())

			// subscription processing
			go func() {
				err := j.Azure.SubscriptionsIterator.ForEach(runLogger, func(subscription *armsubscriptions.Subscription, logger *zap.SugaredLogger) {
					contextLogger := runLogger.With(
						zap.String("subscriptionID", to.String(subscription.SubscriptionID)),
						zap.String("subscriptionName", to.String(subscription.DisplayName)),
					)

					if j.Conf.Janitor.Deployments.Enable {
						j.runDeployments(ctx, contextLogger, subscription, callbackFuncs)
					}

					if j.Conf.Janitor.Resources.Enable {
						j.runResources(ctx, contextLogger, subscription, j.Conf.Janitor.Resources.Filter, callbackFuncs)
					}

					if j.Conf.Janitor.RoleAssignments.Enable {
						j.runRoleAssignments(ctx, contextLogger, subscription, j.Conf.Janitor.RoleAssignments.Filter, callbackFuncs)
					}

					if j.Conf.Janitor.ResourceGroups.Enable {
						j.runResourceGroups(ctx, contextLogger, subscription, j.Conf.Janitor.ResourceGroups.Filter, callbackFuncs)
					}
				})
				if err != nil {
					runLogger.Panic(err)
				}

				close(callbackFuncs)
			}()

			// store metriclists from channel
			callbackFuncList := []func(){}
			for callbackFunc := range callbackFuncs {
				if callbackFunc != nil {
					callbackFuncList = append(callbackFuncList, callbackFunc)
				}
			}

			// after channel is closed: reset metric and set them to the new state
			j.Prometheus.MetricDeployment.Reset()
			j.Prometheus.MetricTtlResources.Reset()
			j.Prometheus.MetricTtlRoleAssignments.Reset()

			for _, callbackFunc := range callbackFuncList {
				callbackFunc()
			}

			duration := time.Since(startTime)
			j.Prometheus.MetricDuration.With(prometheus.Labels{}).Set(duration.Seconds())

			runLogger.With(zap.Float64("duration", duration.Seconds())).Infof("finished run in %s, waiting %s", duration.String(), j.Conf.Janitor.Interval.String())
			time.Sleep(j.Conf.Janitor.Interval)
		}
	}()
}

func (j *Janitor) initAuzreApiVersions() {
	ctx := context.Background()

	j.apiVersionMap = map[string]map[string]string{}

	err := j.Azure.SubscriptionsIterator.ForEach(j.Logger, func(subscription *armsubscriptions.Subscription, logger *zap.SugaredLogger) {
		subscriptionId := to.String(subscription.SubscriptionID)

		// fetch location translation map
		subscriptionClient, err := armsubscriptions.NewClient(j.Azure.Client.GetCred(), j.Azure.Client.NewArmClientOptions())
		if err != nil {
			logger.Panic(err)
		}

		locationPager := subscriptionClient.NewListLocationsPager(*subscription.SubscriptionID, nil)
		locationMap := map[string]string{}
		for locationPager.More() {
			result, err := locationPager.NextPage(ctx)
			if err != nil {
				logger.Panic(err)
			}

			for _, location := range result.Value {
				locationDisplayName := to.String(location.DisplayName)
				locationName := to.String(location.Name)
				locationMap[locationDisplayName] = locationName
			}
		}

		providersClient, err := armresources.NewProvidersClient(*subscription.SubscriptionID, j.Azure.Client.GetCred(), j.Azure.Client.NewArmClientOptions())
		if err != nil {
			logger.Panic(err)
		}

		providerPager := providersClient.NewListPager(nil)
		j.apiVersionMap[subscriptionId] = map[string]string{}
		for providerPager.More() {
			result, err := providerPager.NextPage(ctx)
			if err != nil {
				logger.Panic(err)
			}

			for _, provider := range result.Value {
				if provider.ResourceTypes == nil {
					continue
				}

				for _, resourceType := range provider.ResourceTypes {
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
					for _, val := range resourceType.APIVersions {
						if val == nil {
							continue
						}

						apiVersion := to.String(val)
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
					for _, val := range resourceType.Locations {
						if val == nil {
							continue
						}
						location := to.String(val)

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
	})
	if err != nil {
		j.Logger.Panic(err)
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

func (j *Janitor) checkAzureResourceExpiry(logger *zap.SugaredLogger, resourceType, resourceId string, resourceTags *map[string]*string) (resourceExpireTime *time.Time, resourceExpired bool, resourceTagRewriteNeeded bool) {
	ttlValue := j.getTtlTagFromAzureResource(*resourceTags)

	if ttlValue != nil {
		logger.Debug("checking ttl")

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
				logger.Debug("NOT expired")
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
