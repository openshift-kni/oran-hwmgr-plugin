package api

import (
	"context"

	"github.com/openshift-kni/oran-hwmgr-plugin/adaptors"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/server/api/generated"
)

type InventoryServer struct {
	HwMgrAdaptor *adaptors.HwMgrAdaptorController
}

// InventoryServer implements StrictServerInterface. This ensures that we've conformed to the `StrictServerInterface` with a compile-time check
var _ generated.StrictServerInterface = (*InventoryServer)(nil)

// baseURL is the prefix for all of our supported API endpoints
var baseURL = "/hardware-manager/inventory/v1"
var currentVersion = "1.0.0"

// GetAllVersions handles an API request to fetch all versions
func (i *InventoryServer) GetAllVersions(_ context.Context, _ generated.GetAllVersionsRequestObject) (generated.GetAllVersionsResponseObject, error) {
	// We currently only support a single version
	versions := []generated.APIVersion{
		{
			Version: &currentVersion,
		},
	}
	return generated.GetAllVersions200JSONResponse(generated.APIVersions{
		ApiVersions: &versions,
		UriPrefix:   &baseURL,
	}), nil
}

// GetMinorVersions handles an API request to fetch minor versions
func (i *InventoryServer) GetMinorVersions(_ context.Context, _ generated.GetMinorVersionsRequestObject) (generated.GetMinorVersionsResponseObject, error) {
	// We currently only support a single version
	versions := []generated.APIVersion{
		{
			Version: &currentVersion,
		},
	}
	return generated.GetMinorVersions200JSONResponse(generated.APIVersions{
		ApiVersions: &versions,
		UriPrefix:   &baseURL,
	}), nil
}

func (i *InventoryServer) GetResourcePools(ctx context.Context, request generated.GetResourcePoolsRequestObject) (generated.GetResourcePoolsResponseObject, error) {
	return i.HwMgrAdaptor.GetResourcePools(ctx, request) // nolint: wrapcheck
}

func (i *InventoryServer) GetResourcePool(ctx context.Context, request generated.GetResourcePoolRequestObject) (generated.GetResourcePoolResponseObject, error) {
	// TODO implement me
	return generated.GetResourcePool200JSONResponse{}, nil
}

func (i *InventoryServer) GetResourcePoolResources(ctx context.Context, request generated.GetResourcePoolResourcesRequestObject) (generated.GetResourcePoolResourcesResponseObject, error) {
	// TODO implement me
	return generated.GetResourcePoolResources200JSONResponse([]generated.ResourceInfo{}), nil
}

func (i *InventoryServer) GetResources(ctx context.Context, request generated.GetResourcesRequestObject) (generated.GetResourcesResponseObject, error) {
	return i.HwMgrAdaptor.GetResources(ctx, request) // nolint: wrapcheck
}

func (i *InventoryServer) GetResource(ctx context.Context, request generated.GetResourceRequestObject) (generated.GetResourceResponseObject, error) {
	// TODO implement me
	return generated.GetResource200JSONResponse{}, nil
}
