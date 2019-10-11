package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/Azure/azure-sdk-for-go/profiles/2019-03-01/resources/mgmt/resources"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/subscriptions"
	"github.com/google/logger"
	"github.com/prometheus/client_golang/prometheus"
	"strings"
	"time"
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

func startAzureJanitor() {
	ctx := context.Background()

	go func() {
		for {
			logger.Infof("Starting run")
			Prometheus.MetricTtlResources.Reset()

			for _, subscription := range AzureSubscriptions {

				if !opts.JanitorDisableResourceGroups {
					janitorCleanupResourceGroups(ctx, subscription, opts.janitorFilterResourceGroups)
				}

				if !opts.JanitorDisableResources {
					janitorCleanupResources(ctx, subscription, opts.janitorFilterResources)
				}
			}
			logger.Infof("Finished run, waiting %s", opts.JanitorInterval.String())
			time.Sleep(opts.JanitorInterval)
		}
	}()
}

func janitorCleanupResources(ctx context.Context, subscription subscriptions.Subscription, filter string) {
	client := resources.NewClient(*subscription.SubscriptionID)
	client.Authorizer = AzureAuthorizer

	resourceResult, err := client.ListComplete(ctx, filter, "", nil)

	if err != nil {
		panic(err)
	}

	for _, resource := range *resourceResult.Response().Value {
		resourceType := *resource.Type

		if resource.Tags != nil {
			if janitorCheckAzureResourceExpiry(resourceType, *resource.ID, resource.Tags) {
				logger.Infof("%s: expired, trying to delete", *resource.ID)
				if _, err := client.DeleteByID(ctx, *resource.ID); err == nil {
					// successfully deleted
					logger.Infof("%s: successfully deleted", *resource.ID)

					Prometheus.MetricDeletedResource.With(prometheus.Labels{
						"resourceType": resourceType,
					}).Inc()
				} else {
					// failed delete
					logger.Errorf("%s: ERROR %s", *resource.ID, err)

					Prometheus.MetricErrors.With(prometheus.Labels{
						"resourceType": resourceType,
					}).Inc()
				}
			}
		}
	}
}

func janitorCleanupResourceGroups(ctx context.Context, subscription subscriptions.Subscription, filter string) {
	client := resources.NewGroupsClient(*subscription.SubscriptionID)
	client.Authorizer = AzureAuthorizer

	resourceGroupResult, err := client.ListComplete(ctx, filter, nil)
	if err != nil {
		panic(err)
	}

	for _, resourceGroup := range *resourceGroupResult.Response().Value {
		// resourceGroup.Type is nil
		resourceType := "Microsoft.Resources/resourceGroups"

		if resourceGroup.Tags != nil {
			if janitorCheckAzureResourceExpiry(resourceType, *resourceGroup.ID, resourceGroup.Tags) {
				logger.Infof("%s: expired, trying to delete", *resourceGroup.ID)
				if _, err := client.Delete(ctx, *resourceGroup.Name); err == nil {
					// successfully deleted
					logger.Infof("%s: successfully deleted", *resourceGroup.ID)

					Prometheus.MetricDeletedResource.With(prometheus.Labels{
						"resourceType": resourceType,
					}).Inc()
				} else {
					// failed delete
					logger.Errorf("%s: ERROR %s", *resourceGroup.ID, err)

					Prometheus.MetricErrors.With(prometheus.Labels{
						"resourceType": resourceType,
					}).Inc()
				}
			}
		}
	}
}

func janitorCheckAzureResourceExpiry(resourceType, resourceId string, resourceTags map[string]*string) (resourceExpired bool) {
	ttlValue := janitorAzureResourceGetTtlTag(resourceTags)

	if ttlValue != nil {
		if Verbose {
			logger.Infof("%s: checking ttl", resourceId)
		}

		Prometheus.MetricTtlResources.With(prometheus.Labels{
			"resourceType": resourceType,
		}).Inc()

		tagValueExpired, err := janitorCheckExpiryDate(*ttlValue)

		if err == nil {
			if tagValueExpired {
				if opts.DryRun {
					logger.Infof("%s: expired, but dryrun active", resourceId)
				} else {
					resourceExpired = true
				}
			} else {
				if Verbose {
					logger.Infof("%s: NOT expired", resourceId)
				}
			}
		} else {
			logger.Errorf("%s: ERROR %s", resourceId, err)
		}
	}

	return
}

func janitorAzureResourceGetTtlTag(tags map[string]*string) (ttlValue *string) {
	for tagName, tagValue := range tags {
		if tagName == opts.JanitorTag && tagValue != nil && *tagValue != "" {
			ttlValue = tagValue
		}
	}

	return
}

func janitorCheckExpiryDate(value string) (expired bool, err error) {
	var parsedTime *time.Time
	expired = false

	// sanity checks
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}

	// parse time
	for _, timeFormat := range janitorTimeFormats {
		if val, err := time.Parse(timeFormat, value); err == nil && val.Unix() > 0 {
			parsedTime = &val
			err = nil
			break
		}
	}

	// check if time could be parsed
	if parsedTime != nil {
		// check if parsed time is before NOW -> expired
		expired = parsedTime.Before(time.Now())
	} else {
		err = errors.New(fmt.Sprintf(
			"Unable to parse time '%s'",
			value,
		))
	}

	return
}
