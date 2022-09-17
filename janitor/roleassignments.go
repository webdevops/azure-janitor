package janitor

import (
	"context"
	"time"

	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/subscriptions"                         //nolint:staticcheck
	"github.com/Azure/azure-sdk-for-go/services/preview/authorization/mgmt/2020-04-01-preview/authorization" //nolint:staticcheck
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	azureCommon "github.com/webdevops/go-common/azure"
	prometheusCommon "github.com/webdevops/go-common/prometheus"
)

func (j *Janitor) runRoleAssignments(ctx context.Context, logger *log.Entry, subscription subscriptions.Subscription, filter string, ttlMetricsChan chan<- *prometheusCommon.MetricList) {
	contextLogger := logger.WithField("task", "roleAssignment")

	resourceTtl := prometheusCommon.NewMetricsList()
	resourceType := "Microsoft.Authorization/roleAssignments"

	client := authorization.NewRoleAssignmentsClientWithBaseURI(j.Azure.Client.Environment.ResourceManagerEndpoint, *subscription.SubscriptionID)
	j.decorateAzureAutorest(&client.Client)

	result, err := client.ListComplete(ctx, filter, "")
	if err != nil {
		panic(err.Error())
	}

	for _, roleAssignment := range *result.Response().Value {
		if roleAssignment.RoleDefinitionID == nil || roleAssignment.CreatedOn == nil {
			continue
		}

		azureResource, _ := azureCommon.ParseResourceId(*roleAssignment.Scope)

		roleAssignmentLogger := contextLogger.WithFields(log.Fields{
			"roleAssignmentId": stringPtrToStringLower(roleAssignment.ID),
			"scope":            stringPtrToStringLower(roleAssignment.Scope),
			"principalId":      stringPtrToStringLower(roleAssignment.PrincipalID),
			"principalType":    stringToStringLower(string(roleAssignment.PrincipalType)),
			"roleDefinitionId": stringPtrToStringLower(roleAssignment.RoleDefinitionID),
			"subscriptionID":   stringPtrToStringLower(subscription.SubscriptionID),
			"resourceGroup":    azureResource.ResourceGroup,
		})

		// check if RoleDefinitionID is set
		// do not want to touch other RoleAssignments
		if stringInSlice(*roleAssignment.RoleDefinitionID, j.Conf.Janitor.RoleAssignments.RoleDefintionIds) {
			var roleAssignmentTtl *time.Duration
			if j.Conf.Logger.Verbose {
				roleAssignmentLogger.Infof("checking ttl")
			}

			// detect ttl from description
			if j.Conf.Janitor.RoleAssignments.DescriptionTtlRegExp != nil && roleAssignment.Description != nil {
				descriptionTtlMatch := j.Conf.Janitor.RoleAssignments.DescriptionTtlRegExp.FindSubmatch([]byte(*roleAssignment.Description))

				if len(descriptionTtlMatch) >= 2 {
					if v, err := j.parseExpiryDuration(string(descriptionTtlMatch[1])); err == nil {
						roleAssignmentTtl = v
					}
				}
			}

			// use default ttl if no ttl was detected or ttl is higher then default
			if roleAssignmentTtl == nil || roleAssignmentTtl.Seconds() > j.Conf.Janitor.RoleAssignments.Ttl.Seconds() {
				roleAssignmentTtl = &j.Conf.Janitor.RoleAssignments.Ttl
			}

			// calculate expiry and check if already expired
			roleAssignmentExpiry := roleAssignment.CreatedOn.UTC().Add(*roleAssignmentTtl)
			roleAssignmentExpired := time.Now().After(roleAssignmentExpiry)

			roleAssignmentLogger.Debugf("detected ttl %v", roleAssignmentTtl.String())

			resourceTtl.AddTime(prometheus.Labels{
				"roleAssignmentId": stringPtrToStringLower(roleAssignment.ID),
				"scope":            stringPtrToStringLower(roleAssignment.Scope),
				"principalId":      stringPtrToStringLower(roleAssignment.PrincipalID),
				"principalType":    stringPtrToStringLower(roleAssignment.Type),
				"roleDefinitionId": stringPtrToStringLower(roleAssignment.RoleDefinitionID),
				"subscriptionID":   stringPtrToStringLower(subscription.SubscriptionID),
				"resourceGroup":    azureResource.ResourceGroup,
			}, roleAssignmentExpiry)

			if roleAssignmentExpired {
				if !j.Conf.DryRun {
					roleAssignmentLogger.Infof("expired, trying to delete")
					if _, err := client.DeleteByID(ctx, to.String(roleAssignment.ID), ""); err == nil {
						// successfully deleted
						roleAssignmentLogger.Infof("successfully deleted")

						j.Prometheus.MetricDeletedResource.With(prometheus.Labels{
							"subscriptionID": stringPtrToStringLower(subscription.SubscriptionID),
							"resourceType":   stringToStringLower(resourceType),
						}).Inc()
					} else {
						// failed delete
						roleAssignmentLogger.Error(err.Error())

						j.Prometheus.MetricErrors.With(prometheus.Labels{
							"subscriptionID": stringPtrToStringLower(subscription.SubscriptionID),
							"resourceType":   stringToStringLower(resourceType),
						}).Inc()
					}
				} else {
					roleAssignmentLogger.Infof("expired, but dryrun active")
				}
			} else {
				if j.Conf.Logger.Verbose {
					roleAssignmentLogger.Infof("NOT expired")
				}
			}
		}
	}

	ttlMetricsChan <- resourceTtl
}
