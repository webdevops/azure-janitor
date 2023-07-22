package janitor

import (
	"context"
	"time"

	armauthorization "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armsubscriptions"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/webdevops/go-common/azuresdk/armclient"
	prometheusCommon "github.com/webdevops/go-common/prometheus"
	"github.com/webdevops/go-common/utils/to"
	"go.uber.org/zap"
)

func (j *Janitor) runRoleAssignments(ctx context.Context, logger *zap.SugaredLogger, subscription *armsubscriptions.Subscription, filter string, callback chan<- func()) {
	contextLogger := logger.With(zap.String("task", "roleAssignment"))

	resourceTtl := prometheusCommon.NewMetricsList()
	resourceType := "Microsoft.Authorization/roleAssignments"

	client, err := armauthorization.NewRoleAssignmentsClient(*subscription.SubscriptionID, j.Azure.Client.GetCred(), j.Azure.Client.NewArmClientOptions())
	if err != nil {
		logger.Panic(err)
	}

	pager := client.NewListForScopePager(*subscription.ID, nil)
	for pager.More() {
		result, err := pager.NextPage(ctx)
		if err != nil {
			logger.Panic(err)
		}

		for _, roleAssignment := range result.Value {
			if roleAssignment.Properties.RoleDefinitionID == nil || roleAssignment.Properties.CreatedOn == nil {
				continue
			}

			azureResource, _ := armclient.ParseResourceId(*roleAssignment.Properties.Scope)

			roleAssignmentLogger := contextLogger.With(
				zap.String("roleAssignmentId", stringPtrToStringLower(roleAssignment.ID)),
				zap.String("scope", stringPtrToStringLower(roleAssignment.Properties.Scope)),
				zap.String("principalId", stringPtrToStringLower(roleAssignment.Properties.PrincipalID)),
				zap.String("principalType", stringToStringLower(string(*roleAssignment.Properties.PrincipalType))),
				zap.String("roleDefinitionId", stringPtrToStringLower(roleAssignment.Properties.RoleDefinitionID)),
				zap.String("subscriptionID", stringPtrToStringLower(subscription.SubscriptionID)),
				zap.String("resourceGroup", azureResource.ResourceGroup),
			)

			// check if RoleDefinitionID is set
			// do not want to touch other RoleAssignments
			if stringInSlice(*roleAssignment.Properties.RoleDefinitionID, j.Conf.Janitor.RoleAssignments.RoleDefintionIds) {
				var roleAssignmentTtl *time.Duration
				roleAssignmentLogger.Debug("checking ttl")

				// detect ttl from description
				if j.Conf.Janitor.RoleAssignments.DescriptionTtlRegExp != nil && roleAssignment.Properties.Description != nil {
					descriptionTtlMatch := j.Conf.Janitor.RoleAssignments.DescriptionTtlRegExp.FindSubmatch([]byte(*roleAssignment.Properties.Description))

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
				roleAssignmentExpiry := roleAssignment.Properties.CreatedOn.UTC().Add(*roleAssignmentTtl)
				roleAssignmentExpired := time.Now().After(roleAssignmentExpiry)

				roleAssignmentLogger.Debugf("detected ttl %v", roleAssignmentTtl.String())

				resourceTtl.AddTime(prometheus.Labels{
					"roleAssignmentId": stringPtrToStringLower(roleAssignment.ID),
					"scope":            stringPtrToStringLower(roleAssignment.Properties.Scope),
					"principalId":      stringPtrToStringLower(roleAssignment.Properties.PrincipalID),
					"principalType":    stringPtrToStringLower(roleAssignment.Type),
					"roleDefinitionId": stringPtrToStringLower(roleAssignment.Properties.RoleDefinitionID),
					"subscriptionID":   stringPtrToStringLower(subscription.SubscriptionID),
					"resourceGroup":    azureResource.ResourceGroup,
				}, roleAssignmentExpiry)

				if roleAssignmentExpired {
					if !j.Conf.DryRun {
						roleAssignmentLogger.Infof("expired, trying to delete")
						if _, err := client.DeleteByID(ctx, to.String(roleAssignment.ID), nil); err == nil {
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
					roleAssignmentLogger.Debug("NOT expired")
				}
			}
		}
	}

	callback <- func() {
		resourceTtl.GaugeSet(j.Prometheus.MetricTtlRoleAssignments)
	}
}
