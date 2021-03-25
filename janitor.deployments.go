package main

import (
	"context"
	"github.com/Azure/azure-sdk-for-go/profiles/2020-09-01/resources/mgmt/resources"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/subscriptions"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	prometheusCommon "github.com/webdevops/go-prometheus-common"
	"time"
)

func (j *Janitor) runDeployments(ctx context.Context, subscription subscriptions.Subscription, ttlMetricsChan chan<- *prometheusCommon.MetricList) {
	var deploymentCounter, deploymentFinalCounter int64
	contextLogger := log.WithField("task", "deployment")

	client := resources.NewGroupsClientWithBaseURI(azureEnvironment.ResourceManagerEndpoint, *subscription.SubscriptionID)
	client.Authorizer = azureAuthorizer

	resourceTtl := prometheusCommon.NewMetricsList()

	deploymentClient := resources.NewDeploymentsClient(*subscription.SubscriptionID)
	deploymentClient.Authorizer = azureAuthorizer

	resourceType := "Microsoft.Resources/deployments"

	resourceGroupResult, err := client.ListComplete(ctx, "", nil)
	if err != nil {
		panic(err)
	}

	for _, resourceGroup := range *resourceGroupResult.Response().Value {
		deploymentCounter = 0
		deploymentFinalCounter = 0

		resourceLogger := contextLogger.WithField("resource", *resourceGroup.ID)

		deploymentResult, err := deploymentClient.ListByResourceGroupComplete(ctx, *resourceGroup.Name, "", nil)
		if err != nil {
			panic(err)
		}

		for _, deployment := range *deploymentResult.Response().Value {
			deleteDeployment := false
			deploymentCounter++

			if deploymentCounter >= opts.Janitor.DeploymentsLimit {
				// limit reached
				deleteDeployment = true
			} else if deployment.Properties != nil && deployment.Properties.Timestamp != nil {
				// expire check
				deploymentAge := time.Since(deployment.Properties.Timestamp.Time)
				if deploymentAge.Seconds() > opts.Janitor.DeploymentsTtl.Seconds() {
					deleteDeployment = true
				}
			}

			if !opts.DryRun && deleteDeployment {
				if _, err := deploymentClient.Delete(ctx, *resourceGroup.Name, *deployment.Name); err == nil {
					// successfully deleted
					resourceLogger.Infof("%s: successfully deleted", *deployment.ID)

					Prometheus.MetricDeletedResource.With(prometheus.Labels{
						"resourceType": resourceType,
					}).Inc()
				} else {
					// failed delete
					resourceLogger.Errorf("%s: ERROR %s", *deployment.ID, err)

					Prometheus.MetricErrors.With(prometheus.Labels{
						"resourceType": resourceType,
					}).Inc()
				}
			} else {
				deploymentFinalCounter++
			}
		}

		resourceLogger.Infof("found %v deployments, %v still existing, %v deleted", deploymentCounter, deploymentFinalCounter, deploymentCounter-deploymentFinalCounter)
	}

	ttlMetricsChan <- resourceTtl
}
