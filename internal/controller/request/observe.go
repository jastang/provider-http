package request

import (
	"context"
	"net/http"
	"strings"

	"github.com/arielsepton/provider-http/apis/request/v1alpha1"
	httpClient "github.com/arielsepton/provider-http/internal/clients/http"
	"github.com/arielsepton/provider-http/internal/controller/request/requestgen"
	"github.com/arielsepton/provider-http/internal/json"
	"github.com/arielsepton/provider-http/internal/utils"
	"github.com/pkg/errors"
)

const (
	errObjectNotFound = "object wasn't found"
	errNotValidJSON   = "%s is not a valid JSON string: %s"
)

type ObserveRequestDetails struct {
	Details       httpClient.HttpDetails
	ResponseError error
	Synced        bool
}

// NewObserveRequestDetails is a constructor function that initializes
// an instance of ObserveRequestDetails with default values.
func NewObserve(details httpClient.HttpDetails, resErr error, synced bool) ObserveRequestDetails {
	return ObserveRequestDetails{
		Synced:        synced,
		Details:       details,
		ResponseError: resErr,
	}
}

// NewObserveRequestDetails is a constructor function that initializes
// an instance of ObserveRequestDetails with default values.
func FailedObserve() ObserveRequestDetails {
	return ObserveRequestDetails{
		Synced: false,
	}
}

// isUpToDate checks whether desired spec up to date with the observed state for a given request
func (c *external) isUpToDate(ctx context.Context, cr *v1alpha1.Request) (ObserveRequestDetails, error) {
	if !c.isObjectValidForObservation(cr) {
		return FailedObserve(), errors.New(errObjectNotFound)
	}

	requestDetails, err := c.requestDetails(cr, http.MethodGet)
	if err != nil {
		return FailedObserve(), err
	}

	details, responseErr := c.http.SendRequest(ctx, http.MethodGet, requestDetails.Url, requestDetails.Body, requestDetails.Headers, cr.Spec.ForProvider.InsecureSkipTLSVerify)
	if details.HttpResponse.StatusCode == http.StatusNotFound {
		return FailedObserve(), errors.New(errObjectNotFound)
	}

	desiredState, err := c.desiredState(cr)
	if err != nil {
		return FailedObserve(), err
	}

	return c.compareResponseAndDesiredState(details, responseErr, desiredState)
}

func (c *external) isObjectValidForObservation(cr *v1alpha1.Request) bool {
	return cr.Status.Response.Body != "" &&
		!(cr.Status.RequestDetails.Method == http.MethodPost && utils.IsHTTPError(cr.Status.Response.StatusCode))
}

func (c *external) compareResponseAndDesiredState(details httpClient.HttpDetails, err error, desiredState string) (ObserveRequestDetails, error) {
	observeRequestDetails := NewObserve(details, err, false)

	if json.IsJSONString(details.HttpResponse.Body) && json.IsJSONString(desiredState) {
		responseBodyMap := json.JsonStringToMap(details.HttpResponse.Body)
		desiredStateMap := json.JsonStringToMap(desiredState)
		observeRequestDetails.Synced = json.Contains(responseBodyMap, desiredStateMap) && utils.IsHTTPSuccess(details.HttpResponse.StatusCode)
		return observeRequestDetails, nil
	}

	if !json.IsJSONString(details.HttpResponse.Body) && json.IsJSONString(desiredState) {
		return FailedObserve(), errors.Errorf(errNotValidJSON, "response body", details.HttpResponse.Body)
	}

	if json.IsJSONString(details.HttpResponse.Body) && !json.IsJSONString(desiredState) {
		return FailedObserve(), errors.Errorf(errNotValidJSON, "PUT mapping result", desiredState)
	}

	observeRequestDetails.Synced = strings.Contains(details.HttpResponse.Body, desiredState) && utils.IsHTTPSuccess(details.HttpResponse.StatusCode)
	return observeRequestDetails, nil
}

func (c *external) desiredState(cr *v1alpha1.Request) (string, error) {
	requestDetails, err := c.requestDetails(cr, http.MethodPut)
	return requestDetails.Body, err
}

func (c *external) requestDetails(cr *v1alpha1.Request, method string) (requestgen.RequestDetails, error) {
	mapping, ok := getMappingByMethod(&cr.Spec.ForProvider, method)
	if !ok {
		return requestgen.RequestDetails{}, errors.Errorf(errMappingNotFound, method)
	}

	return generateValidRequestDetails(cr, mapping)
}
