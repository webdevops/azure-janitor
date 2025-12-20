package janitor

import (
	"context"
	"log/slog"
	"strings"
	"time"

	armauthorization "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armsubscriptions"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/webdevops/go-common/azuresdk/armclient"
	"github.com/webdevops/go-common/log/slogger"
	prometheusCommon "github.com/webdevops/go-common/prometheus"
	"github.com/webdevops/go-common/utils/to"
)

func (j *Janitor) runRoleAssignments(ctx context.Context, logger *slogger.Logger, subscription *armsubscriptions.Subscription, filter string, callback chan<- func()) {
	contextLogger := logger.With(slog.String("task", "roleAssignment"))

	resourceTtl := prometheusCommon.NewMetricsList()
	resourceType := "Microsoft.Authorization/roleAssignments"

	client, err := armauthorization.NewRoleAssignmentsClient(*subscription.SubscriptionID, j.Azure.Client.GetCred(), j.Azure.Client.NewArmClientOptions())
	if err != nil {
		panic(err)
	}

	pager := client.NewListForScopePager(*subscription.ID, nil)
	for pager.More() {
		result, err := pager.NextPage(ctx)
		if err != nil {
			panic(err)
		}

		for _, roleAssignment := range result.Value {
			if roleAssignment.Properties.RoleDefinitionID == nil || roleAssignment.Properties.CreatedOn == nil {
				continue
			}

			azureResource, _ := armclient.ParseResourceId(*roleAssignment.Properties.Scope)

			roleAssignmentLogger := contextLogger.With(
				slog.String("roleAssignmentId", to.StringLower(roleAssignment.ID)),
				slog.String("scope", to.StringLower(roleAssignment.Properties.Scope)),
				slog.String("principalId", to.StringLower(roleAssignment.Properties.PrincipalID)),
				slog.String("principalType", strings.ToLower(string(*roleAssignment.Properties.PrincipalType))),
				slog.String("roleDefinitionId", to.StringLower(roleAssignment.Properties.RoleDefinitionID)),
				slog.String("subscriptionID", to.StringLower(subscription.SubscriptionID)),
				slog.String("resourceGroup", azureResource.ResourceGroup),
			)

			// check if roleAssignment is allowed for cleanup
			// do not want to touch other RoleAssignments
			if j.isRoleAssignmentCleanupAllowed(roleAssignment) {
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
					"roleAssignmentId": to.StringLower(roleAssignment.ID),
					"scope":            to.StringLower(roleAssignment.Properties.Scope),
					"principalId":      to.StringLower(roleAssignment.Properties.PrincipalID),
					"principalType":    to.StringLower(roleAssignment.Type),
					"roleDefinitionId": to.StringLower(roleAssignment.Properties.RoleDefinitionID),
					"subscriptionID":   to.StringLower(subscription.SubscriptionID),
					"resourceGroup":    azureResource.ResourceGroup,
				}, roleAssignmentExpiry)

				if roleAssignmentExpired {
					if !j.Conf.DryRun {
						roleAssignmentLogger.Infof("expired, trying to delete")
						if _, err := client.DeleteByID(ctx, to.String(roleAssignment.ID), nil); err == nil {
							// successfully deleted
							roleAssignmentLogger.Infof("successfully deleted")

							j.Prometheus.MetricDeletedResource.With(prometheus.Labels{
								"subscriptionID": to.StringLower(subscription.SubscriptionID),
								"resourceType":   strings.ToLower(resourceType),
							}).Inc()
						} else {
							// failed delete
							roleAssignmentLogger.Error(err.Error())

							j.Prometheus.MetricErrors.With(prometheus.Labels{
								"subscriptionID": to.StringLower(subscription.SubscriptionID),
								"resourceType":   strings.ToLower(resourceType),
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

func (j *Janitor) isRoleAssignmentCleanupAllowed(roleAssignment *armauthorization.RoleAssignment) bool {
	roleDefinitionID := to.StringLower(roleAssignment.Properties.RoleDefinitionID)
	for _, check := range j.Conf.Janitor.RoleAssignments.RoleDefintionIds {
		// sanity check, do not allow empty IDs
		if len(check) == 0 {
			continue
		}
		check = strings.ToLower(check)
		if strings.EqualFold(roleDefinitionID, check) || strings.HasSuffix(roleDefinitionID, check) {
			return true
		}
	}

	return false
}
