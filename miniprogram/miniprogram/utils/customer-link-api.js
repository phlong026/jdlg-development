'use strict';
const api = require('./phase3-api');
module.exports = {
  projectCustomers: query => api.request('/api/v1/miniprogram/tracking/my-visitors/project-customers', 'GET', {}, query || {}),
};
