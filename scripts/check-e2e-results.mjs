import { readFileSync } from 'node:fs';

const events = readFileSync(process.argv[2], 'utf8').trim().split('\n').map(JSON.parse);
const required = ['TestProductWireAndRecovery', 'TestProductRateLimitPerListener',
  'TestProductRestart', 'TestProductRejectsStartupErrors', 'TestProductSmoke',
  'TestProductDualStackWildcardBind'];
if (events.some(e => e.Action === 'skip' || e.Action === 'fail')) {
  throw new Error('Product E2E must not fail or skip');
}
for (const name of required) {
  if (!events.some(e => e.Action === 'pass' && e.Test === name)) {
    throw new Error(`Missing product E2E result: ${name}`);
  }
}
