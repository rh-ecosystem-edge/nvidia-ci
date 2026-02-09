package nfd

import (
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2" //nolint:staticcheck
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
)

func Cleanup(apiClient *clients.Settings) error {
	var errs []error

	By("Deleting NFD CR instance")
	if err := NFDCRDeleteAndWait(apiClient); err != nil {
		errs = append(errs, fmt.Errorf("failed to delete NFD CR: %w", err))
	}

	By("Deleting NFD CSV")
	if err := DeleteNFDCSV(apiClient); err != nil {
		errs = append(errs, fmt.Errorf("failed to delete NFD CSV: %w", err))
	}

	By("Deleting NFD Subscription")
	if err := DeleteNFDSubscription(apiClient); err != nil {
		errs = append(errs, fmt.Errorf("failed to delete NFD Subscription: %w", err))
	}

	By("Deleting NFD OperatorGroup")
	if err := DeleteNFDOperatorGroup(apiClient); err != nil {
		errs = append(errs, fmt.Errorf("failed to delete NFD OperatorGroup: %w", err))
	}

	By("Deleting NFD Namespace")
	if err := DeleteNFDNamespace(apiClient); err != nil {
		errs = append(errs, fmt.Errorf("failed to delete NFD Namespace: %w", err))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
