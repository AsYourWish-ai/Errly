import { Errly } from '@errly/sdk'

const apiKey = process.env.ERRLY_API_KEY
if (!apiKey) {
  console.error('ERRLY_API_KEY is not set')
  process.exit(1)
}

const client = new Errly({
  url: 'http://localhost:5080',
  apiKey,
  project: 'ts-sdk-test',
  environment: 'test',
})

client.captureError(new Error('SDK test error from TypeScript'))
console.log('captured error')

client.captureMessage('TypeScript SDK integration test passed', 'info')
console.log('TypeScript SDK test complete')
