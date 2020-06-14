package main

import (
	"context"
	"github.com/Azure/azure-sdk-for-go/profiles/2019-03-01/resources/mgmt/resources"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/subscriptions"
	"github.com/google/logger"
	"github.com/prometheus/client_golang/prometheus"
	prometheusCommon "github.com/webdevops/go-prometheus-common"
)

func (j *Janitor) runResources(ctx context.Context, subscription subscriptions.Subscription, filter string, ttlMetricsChan chan<- *prometheusCommon.MetricList) {
	client := resources.NewClient(*subscription.SubscriptionID)
	client.Authorizer = AzureAuthorizer

	resourceTtl := prometheusCommon.NewMetricsList()

	resourceResult, err := client.ListComplete(ctx, filter, "", nil)
	if err != nil {
		panic(err)
	}

	for _, resource := range *resourceResult.Response().Value {
		resourceType := *resource.Type
		resourceTypeApiVersion := j.getAzureApiVersionForSubscriptionResourceType(*subscription.SubscriptionID, resourceType)

		if resourceTypeApiVersion != "" && resource.Tags != nil {
			resourceExpiryTime, resourceExpired, resourceTagUpdateNeeded := j.checkAzureResourceExpiry(resourceType, *resource.ID, &resource.Tags)

			if resourceExpiryTime != nil {
				resourceTtl.AddTime(prometheus.Labels{
					"subscriptionID": *subscription.SubscriptionID,
					"resourceID":     *resource.ID,
					"resourceGroup":  extractResourceGroupFromAzureId(*resource.ID),
					"provider":       extractProviderFromAzureId(*resource.ID),
				}, *resourceExpiryTime)
			}

			if !opts.DryRun && resourceTagUpdateNeeded {
				logger.Infof("%s: tag update needed, updating resource", *resource.ID)
				resourceOpts := resources.GenericResource{
					Name: resource.Name,
					Tags: resource.Tags,
				}

				if _, err := client.UpdateByID(ctx, *resource.ID, resourceTypeApiVersion, resourceOpts); err == nil {
					// successfully deleted
					logger.Infof("%s: successfully updated", *resource.ID)
				} else {
					// failed delete
					logger.Errorf("%s: ERROR %s", *resource.ID, err)

					Prometheus.MetricErrors.With(prometheus.Labels{
						"resourceType": resourceType,
					}).Inc()
				}
			}

			if !opts.DryRun && resourceExpired {
				logger.Infof("%s: expired, trying to delete", *resource.ID)
				if _, err := client.DeleteByID(ctx, *resource.ID, resourceTypeApiVersion); err == nil {
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
