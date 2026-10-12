'use strict';
const REQUEST_PATH = '/api/v1/miniprogram/customer-service/consultation-requests';
function optionalConsultationHeaders(path, headers) {
  if (path !== REQUEST_PATH) return headers;
  try {
    const tracking = require('./index');
    const sdk = tracking.consented() && tracking.getSDK();
    const context = sdk && sdk.diagnostics().enabled && sdk.context();
    // No demand text, contact details or private-page events are added. The
    // server separately verifies the signed account and committed lead ID.
    if (context && context.entry_token) return { ...headers, 'X-Tracking-Token': context.entry_token };
  } catch (_) { /* Analytics failure must not block a consultation request. */ }
  return headers;
}
module.exports = { REQUEST_PATH, optionalConsultationHeaders };
