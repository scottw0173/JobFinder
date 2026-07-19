@description('Name of the user-assigned managed identity for the JobFinder Container Apps Job.')
param name string

@description('Azure region for the identity.')
param location string

resource uami 'Microsoft.ManagedIdentity/userAssignedIdentities@2024-11-30' = {
  name: name
  location: location
}

output id string = uami.id
output principalId string = uami.properties.principalId
output clientId string = uami.properties.clientId
output name string = uami.name
