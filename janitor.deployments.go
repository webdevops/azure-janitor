package main

import (
	"context"
	"github.com/Azure/azure-sdk-for-go/profiles/2019-03-01/resources/mgmt/resources"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/subscriptions"
	"github.com/google/logger"
	"github.com/prometheus/client_golang/prometheus"
	"time"
)

func janitorCleanupResourceGroupDeployments(ctx context.Context, subscription subscriptions.Subscription, filter string, ttlMetricsChan chan<- MetricCollectorList) {
	var deploymentCounter, deploymentFinalCounter int64

	client := resources.NewGroupsClient(*subscription.SubscriptionID)
	client.Authorizer = AzureAuthorizer

	resourceTtl := MetricCollectorList{}

	deploymentClient := resources.NewDeploymentsClient(*subscription.SubscriptionID)
	deploymentClient.Authorizer = AzureAuthorizer

	resourceType := "Microsoft.Resources/deployments"

	resourceGroupResult, err := client.ListComplete(ctx, filter, nil)
	if err != nil {
		panic(err)
	}

	for _, resourceGroup := range *resourceGroupResult.Response().Value {
		deploymentCounter = 0
		deploymentFinalCounter = 0

		deploymentResult, err := deploymentClient.ListByResourceGroupComplete(ctx, *resourceGroup.Name, "", nil)
		if err != nil {
			panic(err)
		}

		for _, deployment := range *deploymentResult.Response().Value {
			deleteDeployment := false
			deploymentCounter++

			if deploymentCounter >= opts.JanitorDeploymentsLimit {
				// limit reached
				deleteDeployment = true
			} else if deployment.Properties != nil && deployment.Properties.Timestamp != nil {
				// expire check
				deploymentAge := time.Now().Sub(deployment.Properties.Timestamp.Time)
				if deploymentAge.Seconds() > opts.JanitorDeploymentsTtl.Seconds() {
					deleteDeployment = true
				}
			}

			if !opts.DryRun && deleteDeployment {
				if _, err := deploymentClient.Delete(ctx, *resourceGroup.Name, *deployment.Name); err == nil {
					// successfully deleted
					logger.Infof("%s: successfully deleted", *deployment.ID)

					Prometheus.MetricDeletedResource.With(prometheus.Labels{
						"resourceType": resourceType,
					}).Inc()
				} else {
					// failed delete
					logger.Errorf("%s: ERROR %s", *deployment.ID, err)

					Prometheus.MetricErrors.With(prometheus.Labels{
						"resourceType": resourceType,
					}).Inc()
				}
			} else {
				deploymentFinalCounter++
			}
		}

		logger.Infof("%s: found %v deployments, %v still existing, %v deleted", *resourceGroup.ID, deploymentCounter, deploymentFinalCounter, deploymentCounter-deploymentFinalCounter)
	}

	ttlMetricsChan <- resourceTtl
}
