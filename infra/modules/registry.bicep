@description('Name of the Azure Container Registry.')
param name string

@description('Azure region for the registry.')
param location string

@description('ACR SKU.')
param skuName string = 'Basic'

resource acr 'Microsoft.ContainerRegistry/registries@2023-07-01' = {
  name: name
  location: location
  sku: {
    name: skuName
  }
  properties: {
    // Admin user is a shared-key auth path - disabled to keep this
    // resource keyless-auth-consistent. Pulls happen via the UAMI's
    // AcrPull role assignment instead (see rbac.bicep).
    adminUserEnabled: false
  }
}

output id string = acr.id
output loginServer string = acr.properties.loginServer
output name string = acr.name
