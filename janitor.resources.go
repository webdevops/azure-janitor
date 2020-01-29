package main

import (
	"context"
	"github.com/Azure/azure-sdk-for-go/profiles/2019-03-01/resources/mgmt/resources"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/subscriptions"
	"github.com/google/logger"
	"github.com/prometheus/client_golang/prometheus"
)

func janitorCleanupResources(ctx context.Context, subscription subscriptions.Subscription, filter string, ttlMetricsChan chan<- MetricCollectorList) {
	client := resources.NewClient(*subscription.SubscriptionID)
	client.Authorizer = AzureAuthorizer

	resourceTtl := MetricCollectorList{}

	resourceResult, err := client.ListComplete(ctx, filter, "", nil)

	if err != nil {
		panic(err)
	}

	for _, resource := range *resourceResult.Response().Value {
		resourceType := *resource.Type

		if resource.Tags != nil {
			resourceExpiryTime, resourceExpired := janitorCheckAzureResourceExpiry(resourceType, *resource.ID, resource.Tags)

			if resourceExpiryTime != nil {
				resourceTtl.AddTime(prometheus.Labels{
					"subscriptionID": *subscription.SubscriptionID,
					"resourceID":     *resource.ID,
					"resourceGroup":  extractResourceGroupFromAzureId(*resource.ID),
					"provider":       extractProviderFromAzureId(*resource.ID),
				}, *resourceExpiryTime)
			}

			if !opts.DryRun && resourceExpired {
				logger.Infof("%s: expired, trying to delete", *resource.ID)
				if _, err := client.DeleteByID(ctx, *resource.ID, opts.JanitorResourceApiVersion); err == nil {
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

	ttlMetricsChan <- resourceTtl
}


func janitorAzureResourceGetTtlTag(tags map[string]*string) (ttlValue *string) {
	for tagName, tagValue := range tags {
		if tagName == opts.JanitorTag && tagValue != nil && *tagValue != "" {
			ttlValue = tagValue
		}
	}

	return
}
