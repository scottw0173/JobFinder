@description('Name of the Container Apps Job.')
param name string

@description('Azure region for the Job.')
param location string

@description('Resource ID of the Container Apps managed Environment.')
param environmentId string

@description('Resource ID of the Job\'s user-assigned identity.')
param uamiId string

@description('Client ID of the Job\'s UAMI, injected as AZURE_CLIENT_ID so the Go managed-identity credential picks the right identity.')
param uamiClientId string

@description('ACR login server, e.g. myregistry.azurecr.io.')
param acrLoginServer string

@description('Full container image reference to run, e.g. myregistry.azurecr.io/jobfinder:latest.')
param containerImage string

@description('Azure OpenAI/Foundry account endpoint.')
param openAiEndpoint string

@description('JSON-encoded model list override matching ModelConfig - omit to use the Go code\'s defaultAzureModels.')
param azureModelsJson string = ''

@description('Path inside the container where config files are expected - aspirational: no Blob-backed ConfigSource code exists yet, config_azure.go only reads a local filesystem path today.')
param azureConfigDir string = '/config'

@description('Postgres server FQDN.')
param postgresFqdn string

@description('Postgres database name.')
param postgresDatabaseName string

@description('Name of the Postgres database principal created for this Job\'s identity (see postgres.bicep\'s deploymentScript).')
param postgresAppPrincipalName string

@description('Cron schedule for the daily run - matches AWS template.yaml\'s cron(0 13 * * ? *) UTC time.')
param cronSchedule string = '0 13 * * *'

var baseEnv = [
  {
  // Needed to avoid 400 error during fetch of AAD token
  // without this, you will  get ManagedIdentityCredential error  
  name: 'AZURE_CLIENT_ID'
  value: uamiClientId
  }
  {
    // Azure-exclusive infra by design - CLAUDE.md: "Only the
    // deployment/infra is Azure-exclusive on this branch." Not
    // parametrized; this Job only ever wires the Azure path.
    name: 'CLOUD_PROVIDER'
    value: 'azure'
  }
  {
    name: 'AZURE_CONFIG_DIR'
    value: azureConfigDir
  }
  {
    name: 'AZURE_OPENAI_ENDPOINT'
    value: openAiEndpoint
  }
  {
    // KNOWN, DELIBERATE GAP: wireAzure() in main.go still expects a plain
    // connection-string env var (pgxpool.New(ctx, dsn)) with no AAD-token
    // wiring - that Go-side work is explicitly deferred (see plan). Since
    // postgres.bicep disables password auth entirely, there is no password
    // to put here even if the hard rule allowed it. This DSN is
    // syntactically complete but will fail at connection time
    // (pool.Ping) until the deferred Go-side AAD-token-as-password work
    // lands. Not a bug - an intentional, visible failure rather than a
    // hardcoded password.
    name: 'POSTGRES_DSN'
    value: 'postgres://${postgresAppPrincipalName}@${postgresFqdn}:5432/${postgresDatabaseName}?sslmode=require'
  }
]

var modelsEnv = empty(azureModelsJson) ? [] : [
  {
    name: 'AZURE_MODELS'
    value: azureModelsJson
  }
]

resource job 'Microsoft.App/jobs@2024-03-01' = {
  name: name
  location: location
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${uamiId}': {}
    }
  }
  properties: {
    environmentId: environmentId
    configuration: {
      triggerType: 'Schedule'
      scheduleTriggerConfig: {
        cronExpression: cronSchedule
        parallelism: 1
        replicaCompletionCount: 1
      }
      replicaTimeout: 1800
      replicaRetryLimit: 0
      registries: [
        {
          server: acrLoginServer
          identity: uamiId
        }
      ]
    }
    template: {
      containers: [
        {
          name: 'jobfinder'
          image: containerImage
          resources: {
            cpu: json('1.0')
            memory: '2Gi'
          }
          env: concat(baseEnv, modelsEnv)
        }
      ]
    }
  }
}

output name string = job.name
output id string = job.id
