@description('Name of the storage account. Must be globally unique, lowercase, 3-24 chars.')
param name string

@description('Azure region for the storage account.')
param location string

@description('Name of the blob container that will hold ConfigSource files (instructions.md, sources.json, filterKeywords.json). No consuming Go code exists yet - this mirrors AWS S3CONFIG for future use.')
param containerName string = 'config'

resource account 'Microsoft.Storage/storageAccounts@2023-01-01' = {
  name: name
  location: location
  sku: {
    name: 'Standard_LRS'
  }
  kind: 'StorageV2'
  properties: {
    // RBAC/Entra-only data-plane access - keyless-auth hard rule applied
    // to storage, not just compute.
    allowSharedKeyAccess: false
    minimumTlsVersion: 'TLS1_2'
  }
}

resource blobService 'Microsoft.Storage/storageAccounts/blobServices@2023-01-01' = {
  parent: account
  name: 'default'
}

resource container 'Microsoft.Storage/storageAccounts/blobServices/containers@2023-01-01' = {
  parent: blobService
  name: containerName
}

output id string = account.id
output name string = account.name
output containerName string = container.name
