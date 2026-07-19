@description('Name of the Key Vault. Must be globally unique, 3-24 chars.')
param name string

@description('Azure region for the Key Vault.')
param location string

@description('Azure AD tenant ID.')
param tenantId string = subscription().tenantId

// Empty vault - no secret values seeded here. Populating a real secret
// (e.g. the Google Sheets service-account key) is deferred to the
// separate Go-side Secrets-wiring effort; hardcoding a value into this
// Bicep template would itself violate "no keys in Bicep."
resource vault 'Microsoft.KeyVault/vaults@2023-07-01' = {
  name: name
  location: location
  properties: {
    tenantId: tenantId
    sku: {
      family: 'A'
      name: 'standard'
    }
    enableRbacAuthorization: true
  }
}

output id string = vault.id
output name string = vault.name
output uri string = vault.properties.vaultUri
