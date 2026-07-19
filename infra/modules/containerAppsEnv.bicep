@description('Name of the Container Apps managed Environment.')
param name string

@description('Azure region for the environment.')
param location string

@description('Name of an existing Log Analytics workspace in the same resource group to wire up for Container Apps logging.')
param logAnalyticsWorkspaceName string

resource law 'Microsoft.OperationalInsights/workspaces@2023-09-01' existing = {
  name: logAnalyticsWorkspaceName
}

resource env 'Microsoft.App/managedEnvironments@2024-03-01' = {
  name: name
  location: location
  properties: {
    appLogsConfiguration: {
      destination: 'log-analytics'
      logAnalyticsConfiguration: {
        customerId: law.properties.customerId
        sharedKey: law.listKeys().primarySharedKey
      }
    }
  }
}

output id string = env.id
output name string = env.name
