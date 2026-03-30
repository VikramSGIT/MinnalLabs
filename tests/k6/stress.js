import http from 'k6/http';
import encoding from 'k6/encoding';
import { check, group, sleep } from 'k6';
import execution from 'k6/execution';
import { Counter, Trend } from 'k6/metrics';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.0.4/index.js';

const BASE_URL = stripTrailingSlash(__ENV.BASE_URL || 'http://localhost:8080');
const SESSION_COOKIE_NAME = __ENV.SESSION_COOKIE_NAME || 'user_session';
const OAUTH_CLIENT_ID = __ENV.OAUTH_CLIENT_ID || 'google-client';
const OAUTH_CLIENT_SECRET = __ENV.OAUTH_CLIENT_SECRET || '';
const OAUTH_REDIRECT_URI = __ENV.OAUTH_REDIRECT_URI || 'http://127.0.0.1/oauth/callback';
const PRODUCT_NAME = __ENV.K6_PRODUCT_NAME || 'ml-smart-sensor-v1';
const DEFAULT_PRODUCT_ID = Number(__ENV.K6_PRODUCT_ID || '1');
const DEVICE_PUBLIC_KEY = __ENV.K6_DEVICE_PUBLIC_KEY || 'AQbnaqQshSiDwqVRxeH8lTij1x49dJjzhQqAwtbW4EI=';
const DUMMY_FIRMWARE = open('./testdata/dummy.bin', 'b');
const SUMMARY_PATH = __ENV.K6_SUMMARY_PATH || './artifacts/k6-summary.json';
const TEST_RUN_ID = __ENV.K6_RUN_ID || `${Date.now()}`;
const USER_FLOW_VUS = normalizeIntegerEnv(__ENV.K6_USER_FLOW_VUS, 100, 1);
const USER_FLOW_ITERATIONS = normalizeIntegerEnv(__ENV.K6_USER_FLOW_ITERATIONS, 100, 1);
const USER_FLOW_MAX_DURATION = __ENV.K6_USER_FLOW_MAX_DURATION || '20m';
const HOME_BURST_VUS = normalizeIntegerEnv(__ENV.K6_HOME_BURST_VUS, 100, 0);
const HOME_BURST_START = __ENV.K6_HOME_BURST_START || '10s';
const HOME_BURST_ITERATIONS = normalizeIntegerEnv(__ENV.K6_HOME_BURST_ITERATIONS, 1, 1);
const HOME_BURST_MAX_DURATION = __ENV.K6_HOME_BURST_MAX_DURATION || '20m';
const FULFILLMENT_PARALLEL_VUS = normalizeIntegerEnv(__ENV.K6_FULFILLMENT_PARALLEL_VUS, 100, 0);
const FULFILLMENT_PARALLEL_START = __ENV.K6_FULFILLMENT_START || '10s';
const FULFILLMENT_PARALLEL_ITERATIONS = normalizeIntegerEnv(__ENV.K6_FULFILLMENT_PARALLEL_ITERATIONS, 1000, 1);
const FULFILLMENT_PARALLEL_MAX_DURATION = __ENV.K6_FULFILLMENT_MAX_DURATION || '10m';
const DEVICE_STREAM_VUS = normalizeIntegerEnv(__ENV.K6_DEVICE_STREAM_VUS, 10, 0);
const DEVICE_STREAM_ITERATIONS = normalizeIntegerEnv(__ENV.K6_DEVICE_STREAM_ITERATIONS, 20, 1);
const DEVICE_STREAM_START = __ENV.K6_DEVICE_STREAM_START || '10s';
const DEVICE_STREAM_DELETE_START = __ENV.K6_DEVICE_STREAM_DELETE_START || '30s';
const DEVICE_STREAM_INTERVAL_MS = normalizeNumberEnv(__ENV.K6_DEVICE_STREAM_INTERVAL_MS, 1000, 0);
const DEVICE_STREAM_MAX_DURATION = __ENV.K6_DEVICE_STREAM_MAX_DURATION || '10m';
const ASYNC_HOME_READY_TIMEOUT_MS = normalizeNumberEnv(__ENV.ASYNC_HOME_READY_TIMEOUT_MS, 90000, 1000);
const ASYNC_HOME_READY_POLL_MS = normalizeNumberEnv(__ENV.ASYNC_HOME_READY_POLL_MS, 2000, 250);
const ASYNC_HOME_EARLY_READY_CHECK_MS = normalizeNumberEnv(__ENV.ASYNC_HOME_EARLY_READY_CHECK_MS, 9000, 0);

const timings = {
  enrollUser: new Trend('api_enroll_user_duration', true),
  loginPage: new Trend('login_page_duration', true),
  formLogin: new Trend('login_form_duration', true),
  sessionLogin: new Trend('api_session_login_duration', true),
  sessionMe: new Trend('api_session_me_duration', true),
  sessionLogout: new Trend('api_session_logout_duration', true),
  enrollHome: new Trend('api_enroll_home_duration', true),
  listHomes: new Trend('api_enroll_homes_duration', true),
  asyncHomeReady: new Trend('async_home_ready_duration', true),
  asyncHomeReadyPolls: new Trend('async_home_ready_polls'),
  enrollDevice: new Trend('api_enroll_device_duration', true),
  listHomeDevices: new Trend('api_home_devices_duration', true),
  deviceStatus: new Trend('api_device_status_duration', true),
  deviceUpdate: new Trend('api_device_update_duration', true),
  deleteDevice: new Trend('api_delete_device_duration', true),
  deleteHome: new Trend('api_delete_home_duration', true),
  adminProducts: new Trend('api_admin_products_duration', true),
  adminRollouts: new Trend('api_admin_rollouts_duration', true),
  adminFirmware: new Trend('api_admin_firmware_duration', true),
  adminRollout: new Trend('api_admin_rollout_duration', true),
  oauthAuthorize: new Trend('api_oauth_authorize_duration', true),
  oauthToken: new Trend('api_oauth_token_duration', true),
  googleSync: new Trend('api_google_sync_duration', true),
  googleExecute: new Trend('api_google_execute_duration', true),
  googleQuery: new Trend('api_google_query_duration', true),
  googleDisconnect: new Trend('api_google_disconnect_duration', true),
};

const asyncCounters = {
  homeReadySuccess: new Counter('async_home_ready_success'),
  homeReadyTimeout: new Counter('async_home_ready_timeout'),
  homeReadyFailedState: new Counter('async_home_ready_failed_state'),
  homeReadyEarly: new Counter('async_home_ready_early'),
};

export const options = {
  insecureSkipTLSVerify: true,
  scenarios: buildScenarios(),
  thresholds: {
    http_req_failed: ['rate<0.10'],
    http_req_duration: ['p(95)<2000'],
    async_home_ready_timeout: ['count==0'],
    async_home_ready_failed_state: ['count==0'],
    async_home_ready_early: ['count==0'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
};

function stripTrailingSlash(value) {
  return value.replace(/\/+$/, '');
}

function normalizeNumberEnv(rawValue, fallback, minimum) {
  if (rawValue === undefined || rawValue === null || rawValue === '') {
    return fallback;
  }
  const parsed = Number(rawValue);
  if (!Number.isFinite(parsed)) {
    return fallback;
  }
  return Math.max(minimum, parsed);
}

function normalizeIntegerEnv(rawValue, fallback, minimum) {
  return Math.floor(normalizeNumberEnv(rawValue, fallback, minimum));
}

function buildScenarios() {
  const scenarios = {
    user_flow: {
      executor: 'shared-iterations',
      exec: 'userFlow',
      vus: USER_FLOW_VUS,
      iterations: USER_FLOW_ITERATIONS,
      maxDuration: USER_FLOW_MAX_DURATION,
      gracefulStop: '30s',
    },
  };

  if (HOME_BURST_VUS > 0) {
    scenarios.async_home_burst = {
      executor: 'per-vu-iterations',
      exec: 'asyncHomeBurst',
      startTime: HOME_BURST_START,
      vus: HOME_BURST_VUS,
      iterations: HOME_BURST_ITERATIONS,
      maxDuration: HOME_BURST_MAX_DURATION,
    };
  }

  if (FULFILLMENT_PARALLEL_VUS > 0) {
    scenarios.fulfillment_parallel = {
      executor: 'shared-iterations',
      exec: 'fulfillmentParallel',
      startTime: FULFILLMENT_PARALLEL_START,
      vus: FULFILLMENT_PARALLEL_VUS,
      iterations: FULFILLMENT_PARALLEL_ITERATIONS,
      maxDuration: FULFILLMENT_PARALLEL_MAX_DURATION,
    };
  }

  if (DEVICE_STREAM_VUS > 0) {
    scenarios.device_stream_enroll = {
      executor: 'per-vu-iterations',
      exec: 'deviceEnrollStream',
      startTime: DEVICE_STREAM_START,
      vus: DEVICE_STREAM_VUS,
      iterations: DEVICE_STREAM_ITERATIONS,
      maxDuration: DEVICE_STREAM_MAX_DURATION,
    };
    scenarios.device_stream_cleanup = {
      executor: 'per-vu-iterations',
      exec: 'deviceCleanupStream',
      startTime: DEVICE_STREAM_DELETE_START,
      vus: DEVICE_STREAM_VUS,
      iterations: DEVICE_STREAM_ITERATIONS,
      maxDuration: DEVICE_STREAM_MAX_DURATION,
    };
  }

  return scenarios;
}

function url(path) {
  return `${BASE_URL}${path}`;
}

function requestParams(name, extra = {}) {
  const params = {
    tags: {
      name,
      endpoint: name,
      ...(extra.tags || {}),
    },
  };

  if (extra.headers) {
    params.headers = extra.headers;
  }
  if (extra.cookies) {
    params.cookies = extra.cookies;
  }
  if (typeof extra.redirects === 'number') {
    params.redirects = extra.redirects;
  }

  return params;
}

function jsonParams(name, extra = {}) {
  return requestParams(name, {
    ...extra,
    headers: {
      'Content-Type': 'application/json',
      ...(extra.headers || {}),
    },
  });
}

function sessionCookies(sessionToken) {
  return {
    [SESSION_COOKIE_NAME]: {
      value: sessionToken,
      replace: true,
    },
  };
}

function bearerHeaders(accessToken, headers = {}) {
  return {
    Authorization: `Bearer ${accessToken}`,
    ...headers,
  };
}

function addTiming(metric, response) {
  if (metric && response && response.timings) {
    metric.add(response.timings.duration);
  }
}

function logFailure(label, response) {
  const body = response && typeof response.body === 'string' ? response.body.slice(0, 400) : '';
  console.error(`${label} failed with status ${response && response.status}: ${body}`);
}

function expectStatus(response, expected, label) {
  const expectedStatuses = Array.isArray(expected) ? expected : [expected];
  const ok = check(response, {
    [`${label} returned expected status`]: (r) => expectedStatuses.includes(r.status),
  });
  if (!ok) {
    logFailure(label, response);
  }
  return ok;
}

function expectCondition(condition, label) {
  const ok = check(
    { ok: condition },
    {
      [label]: (value) => value.ok === true,
    },
  );
  if (!ok) {
    console.error(`${label} failed`);
  }
  return ok;
}

function parseJSON(response, label) {
  try {
    return response.json();
  } catch (error) {
    console.error(`${label} returned invalid JSON: ${error}`);
    return null;
  }
}

function sessionTokenFromResponse(response) {
  const cookies = response.cookies[SESSION_COOKIE_NAME];
  if (!cookies || cookies.length === 0) {
    return '';
  }
  return cookies[0].value;
}

function uniqueSuffix(prefix) {
  const vu = typeof __VU !== 'undefined' ? __VU : 0;
  const iter = typeof __ITER !== 'undefined' ? __ITER : 0;
  return `${prefix}-${TEST_RUN_ID}-${vu}-${iter}-${Date.now()}-${Math.floor(Math.random() * 100000)}`;
}

function createUserCredentials(prefix) {
  return {
    username: uniqueSuffix(prefix).replace(/[^a-zA-Z0-9_]+/g, '_').slice(0, 48),
    password: 'StressTestPass123!',
  };
}

function registerUser(credentials) {
  const name = 'POST /api/enroll/user';
  const response = http.post(
    url('/api/enroll/user'),
    JSON.stringify(credentials),
    jsonParams(name),
  );
  addTiming(timings.enrollUser, response);
  return expectStatus(response, 201, name);
}

function loadLoginPage() {
  const name = 'GET /login';
  const response = http.get(
    url('/login?redirect=/'),
    requestParams(name),
  );
  addTiming(timings.loginPage, response);
  return {
    ok: expectStatus(response, 200, name),
  };
}

function submitLoginForm(username, password) {
  const name = 'POST /login';
  const response = http.post(
    url('/login'),
    {
      username,
      password,
      redirect: '/',
    },
    requestParams(name, {
      headers: {
        'Content-Type': 'application/x-www-form-urlencoded',
      },
      redirects: 0,
    }),
  );
  addTiming(timings.formLogin, response);
  return {
    ok: expectStatus(response, [302, 303], name),
  };
}

function loginSession(username, password) {
  const name = 'POST /api/session/login';
  const response = http.post(
    url('/api/session/login'),
    JSON.stringify({ username, password }),
    jsonParams(name),
  );
  addTiming(timings.sessionLogin, response);

  const ok = expectStatus(response, 200, name);
  const payload = ok ? parseJSON(response, name) : null;
  const sessionToken = sessionTokenFromResponse(response);
  if (ok && !sessionToken) {
    console.error(`${name} did not return the ${SESSION_COOKIE_NAME} cookie`);
  }

  return {
    ok: ok && sessionToken !== '',
    payload,
    response,
    sessionToken,
  };
}

function logoutSession(sessionToken, allowUnauthorized = false) {
  const name = 'POST /api/session/logout';
  const response = http.post(
    url('/api/session/logout'),
    null,
    requestParams(name, {
      cookies: sessionCookies(sessionToken),
    }),
  );
  addTiming(timings.sessionLogout, response);
  const expectedStatus = allowUnauthorized ? [200, 401] : 200;
  return expectStatus(response, expectedStatus, name);
}

function listProducts(sessionToken) {
  const name = 'GET /api/admin/products';
  const response = http.get(
    url('/api/admin/products'),
    requestParams(name, {
      cookies: sessionCookies(sessionToken),
    }),
  );
  addTiming(timings.adminProducts, response);
  const ok = expectStatus(response, 200, name);
  return {
    ok,
    payload: ok ? parseJSON(response, name) : null,
  };
}

function selectProduct(products) {
  if (!Array.isArray(products) || products.length === 0) {
    throw new Error('no products were returned by GET /api/admin/products');
  }

  const productByName = products.find((product) => product.name === PRODUCT_NAME);
  if (productByName) {
    return productByName;
  }

  const productByID = products.find((product) => Number(product.product_id) === DEFAULT_PRODUCT_ID);
  if (productByID) {
    return productByID;
  }

  return products[0];
}

function enrollHome(sessionToken, nameSuffix) {
  const name = 'POST /api/enroll/home';
  const response = http.post(
    url('/api/enroll/home'),
    JSON.stringify({
      name: `k6-home-${nameSuffix}`,
      wifi_ssid: 'k6-ssid',
      wifi_password: 'k6-password',
    }),
    jsonParams(name, {
      cookies: sessionCookies(sessionToken),
    }),
  );
  addTiming(timings.enrollHome, response);
  const ok = expectStatus(response, 201, name);
  const payload = ok ? parseJSON(response, name) : null;

  return {
    ok,
    payload,
    homeID: payload ? Number(payload.home_id) : 0,
  };
}

function listHomes(sessionToken) {
  const name = 'GET /api/enroll/homes';
  const response = http.get(
    url('/api/enroll/homes'),
    requestParams(name, {
      cookies: sessionCookies(sessionToken),
    }),
  );
  addTiming(timings.listHomes, response);
  const ok = expectStatus(response, 200, name);
  const payload = ok ? parseJSON(response, name) : null;
  return { ok, payload };
}

function homeProvisionState(home) {
  return String((home && home.mqtt_provision_state) || '').toLowerCase();
}

function findHomeByID(homes, homeID) {
  if (!Array.isArray(homes)) {
    return null;
  }
  return homes.find((homeItem) => Number(homeItem.home_id) === Number(homeID)) || null;
}

function lookupHome(sessionToken, homeID) {
  const homes = listHomes(sessionToken);
  if (!homes.ok || !Array.isArray(homes.payload)) {
    return { ok: false, home: null };
  }
  return {
    ok: true,
    home: findHomeByID(homes.payload, homeID),
  };
}

function expectHomeNotReadyBeforeDelay(sessionToken, homeID, contextLabel, startedAt) {
  if (ASYNC_HOME_EARLY_READY_CHECK_MS <= 0) {
    return true;
  }

  const elapsed = Date.now() - startedAt;
  const remaining = ASYNC_HOME_EARLY_READY_CHECK_MS - elapsed;
  if (remaining > 0) {
    sleep(remaining / 1000);
  }

  const lookup = lookupHome(sessionToken, homeID);
  if (!lookup.ok || !lookup.home) {
    console.error(`Unable to confirm delayed readiness for ${contextLabel} (home ${homeID})`);
    return false;
  }

  const state = homeProvisionState(lookup.home);
  const ok = state !== 'ready';
  if (!ok) {
    asyncCounters.homeReadyEarly.add(1);
    console.error(
      `Async home readiness became ready too early for ${contextLabel} (home ${homeID}) before ${ASYNC_HOME_EARLY_READY_CHECK_MS}ms`,
    );
  }
  return expectCondition(ok, 'Async home stayed pending before the 10s queue window elapsed');
}

function waitForHomeReady(sessionToken, homeID, contextLabel, startedAt = Date.now()) {
  const deadline = startedAt + ASYNC_HOME_READY_TIMEOUT_MS;
  let polls = 0;
  let lastState = '';
  let lastError = '';

  while (Date.now() <= deadline) {
    const homes = listHomes(sessionToken);
    polls += 1;

    if (homes.ok && Array.isArray(homes.payload)) {
      const home = homes.payload.find((homeItem) => Number(homeItem.home_id) === Number(homeID));
      if (home) {
        lastState = homeProvisionState(home);
        lastError = String(home.mqtt_provision_error || '');

        if (lastState === 'ready') {
          const elapsed = Date.now() - startedAt;
          timings.asyncHomeReady.add(elapsed);
          timings.asyncHomeReadyPolls.add(polls);
          asyncCounters.homeReadySuccess.add(1);
          return {
            ok: true,
            polls,
            elapsed,
            state: lastState,
          };
        }

        if (lastState === 'failed' || lastState === 'deleting') {
          const elapsed = Date.now() - startedAt;
          timings.asyncHomeReady.add(elapsed);
          timings.asyncHomeReadyPolls.add(polls);
          asyncCounters.homeReadyFailedState.add(1);
          console.error(
            `Async home readiness failed for ${contextLabel} (home ${homeID}) with state ${lastState}: ${lastError}`,
          );
          return {
            ok: false,
            polls,
            elapsed,
            state: lastState,
            error: lastError,
          };
        }
      }
    }

    if (Date.now() + ASYNC_HOME_READY_POLL_MS > deadline) {
      break;
    }
    sleep(ASYNC_HOME_READY_POLL_MS / 1000);
  }

  const elapsed = Date.now() - startedAt;
  timings.asyncHomeReady.add(elapsed);
  timings.asyncHomeReadyPolls.add(polls);
  asyncCounters.homeReadyTimeout.add(1);
  console.error(
    `Async home readiness timed out for ${contextLabel} (home ${homeID}) after ${elapsed}ms; last state=${lastState || 'unknown'} ${lastError}`,
  );
  return {
    ok: false,
    polls,
    elapsed,
    state: lastState || 'timeout',
    error: lastError,
  };
}

function waitForHomeReadyAfterDelay(sessionToken, homeID, contextLabel, startedAt = Date.now()) {
  if (!expectHomeNotReadyBeforeDelay(sessionToken, homeID, contextLabel, startedAt)) {
    return {
      ok: false,
      polls: 0,
      elapsed: Date.now() - startedAt,
      state: 'ready-too-early',
    };
  }
  return waitForHomeReady(sessionToken, homeID, contextLabel, startedAt);
}

function enrollNamedDevice(sessionToken, homeID, productID, productName, deviceName) {
  const name = 'POST /api/enroll/device';
  const response = http.post(
    url('/api/enroll/device'),
    JSON.stringify({
      home_id: homeID,
      name: deviceName,
      product_id: productID,
      product_name: productName,
      device_public_key: DEVICE_PUBLIC_KEY,
    }),
    jsonParams(name, {
      cookies: sessionCookies(sessionToken),
    }),
  );
  addTiming(timings.enrollDevice, response);
  const ok = expectStatus(response, 201, name);
  const payload = ok ? parseJSON(response, name) : null;

  return {
    ok,
    payload,
    deviceID: payload ? Number(payload.device_id) : 0,
  };
}

function enrollDevice(sessionToken, homeID, productID, productName, nameSuffix) {
  return enrollNamedDevice(
    sessionToken,
    homeID,
    productID,
    productName,
    `k6-device-${nameSuffix}`,
  );
}

function listHomeDevices(sessionToken, homeID) {
  const name = 'GET /api/enroll/home/:homeID/devices';
  const response = http.get(
    url(`/api/enroll/home/${homeID}/devices`),
    requestParams(name, {
      cookies: sessionCookies(sessionToken),
    }),
  );
  addTiming(timings.listHomeDevices, response);
  const ok = expectStatus(response, 200, name);
  const payload = ok ? parseJSON(response, name) : null;
  return { ok, payload };
}

function findDeviceByName(devices, deviceName) {
  if (!Array.isArray(devices)) {
    return null;
  }
  return devices.find((device) => String(device.name || '') === String(deviceName)) || null;
}

function deviceStreamDeviceName(iterationIndex) {
  return `k6-stream-device-${TEST_RUN_ID}-${iterationIndex}`;
}

function paceDeviceStream(iterationsPerVU) {
  if (DEVICE_STREAM_INTERVAL_MS <= 0) {
    return;
  }
  if (execution.vu.iterationInScenario >= iterationsPerVU - 1) {
    return;
  }
  sleep(DEVICE_STREAM_INTERVAL_MS / 1000);
}

function getDeviceStatus(sessionToken, deviceID) {
  const name = 'GET /api/enroll/device/:deviceID/status';
  const response = http.get(
    url(`/api/enroll/device/${deviceID}/status`),
    requestParams(name, {
      cookies: sessionCookies(sessionToken),
    }),
  );
  addTiming(timings.deviceStatus, response);
  const ok = expectStatus(response, 200, name);
  const payload = ok ? parseJSON(response, name) : null;
  return { ok, payload };
}

function uploadFirmware(sessionToken, productID, version) {
  const name = 'POST /api/admin/products/:productID/firmware';
  const response = http.post(
    url(`/api/admin/products/${productID}/firmware`),
    {
      version,
      file: http.file(DUMMY_FIRMWARE, 'dummy.ota.bin', 'application/octet-stream'),
    },
    requestParams(name, {
      cookies: sessionCookies(sessionToken),
    }),
  );
  addTiming(timings.adminFirmware, response);
  const ok = expectStatus(response, 200, name);
  const payload = ok ? parseJSON(response, name) : null;
  return { ok, payload };
}

function rolloutFirmware(sessionToken, productID) {
  const name = 'POST /api/admin/products/:productID/rollout';
  const response = http.post(
    url(`/api/admin/products/${productID}/rollout`),
    JSON.stringify({
      batch_percentage: 100,
      batch_interval_value: 1,
      batch_interval_unit: 'hours',
    }),
    jsonParams(name, {
      cookies: sessionCookies(sessionToken),
    }),
  );
  addTiming(timings.adminRollout, response);
  const ok = expectStatus(response, 200, name);
  const payload = ok ? parseJSON(response, name) : null;
  return { ok, payload };
}

function listRollouts(sessionToken, productID) {
  const name = 'GET /api/admin/products/:productID/rollouts';
  const response = http.get(
    url(`/api/admin/products/${productID}/rollouts`),
    requestParams(name, {
      cookies: sessionCookies(sessionToken),
    }),
  );
  addTiming(timings.adminRollouts, response);
  const ok = expectStatus(response, 200, name);
  const payload = ok ? parseJSON(response, name) : null;
  return { ok, payload };
}

function triggerDeviceUpdate(sessionToken, deviceID) {
  const name = 'POST /api/enroll/device/:deviceID/update';
  const response = http.post(
    url(`/api/enroll/device/${deviceID}/update`),
    null,
    requestParams(name, {
      cookies: sessionCookies(sessionToken),
    }),
  );
  addTiming(timings.deviceUpdate, response);
  const ok = expectStatus(response, 200, name);
  const payload = ok ? parseJSON(response, name) : null;
  return { ok, payload };
}

function deleteDevice(sessionToken, deviceID) {
  const name = 'DELETE /api/enroll/device/:deviceID';
  const response = http.del(
    url(`/api/enroll/device/${deviceID}`),
    null,
    requestParams(name, {
      cookies: sessionCookies(sessionToken),
    }),
  );
  addTiming(timings.deleteDevice, response);
  return expectStatus(response, 200, name);
}

function deleteDeviceByName(sessionToken, homeID, deviceName) {
  const devices = listHomeDevices(sessionToken, homeID);
  if (!devices.ok || !Array.isArray(devices.payload)) {
    return {
      ok: false,
      found: false,
      deviceID: 0,
    };
  }

  const device = findDeviceByName(devices.payload, deviceName);
  if (!device) {
    return {
      ok: false,
      found: false,
      deviceID: 0,
    };
  }

  const deviceID = Number(device.device_id);
  return {
    ok: deleteDevice(sessionToken, deviceID),
    found: true,
    deviceID,
  };
}

function deleteDeviceByNameWithRetry(sessionToken, homeID, deviceName, attempts = 5, intervalMs = 500) {
  let result = {
    ok: false,
    found: false,
    deviceID: 0,
  };

  for (let attempt = 0; attempt < attempts; attempt += 1) {
    result = deleteDeviceByName(sessionToken, homeID, deviceName);
    if (result.ok || result.found) {
      return result;
    }
    if (attempt < attempts - 1 && intervalMs > 0) {
      sleep(intervalMs / 1000);
    }
  }

  return result;
}

function deleteHome(sessionToken, homeID) {
  const name = 'DELETE /api/enroll/home/:homeID';
  const response = http.del(
    url(`/api/enroll/home/${homeID}`),
    null,
    requestParams(name, {
      cookies: sessionCookies(sessionToken),
    }),
  );
  addTiming(timings.deleteHome, response);
  return expectStatus(response, 200, name);
}

function authorizeCode(sessionToken, stateValue) {
  const name = 'GET /oauth/authorize';
  const query = [
    'response_type=code',
    `client_id=${encodeURIComponent(OAUTH_CLIENT_ID)}`,
    `redirect_uri=${encodeURIComponent(OAUTH_REDIRECT_URI)}`,
    `state=${encodeURIComponent(stateValue)}`,
  ].join('&');
  const response = http.get(
    url(`/oauth/authorize?${query}`),
    requestParams(name, {
      cookies: sessionCookies(sessionToken),
      redirects: 0,
    }),
  );
  addTiming(timings.oauthAuthorize, response);
  const ok = expectStatus(response, [302, 303], name);
  if (!ok) {
    return { ok: false, code: '' };
  }

  const location = response.headers.Location || response.headers.location || '';
  if (!location || location.includes('/login?redirect=')) {
    console.error(`${name} redirected to login instead of the OAuth redirect URI`);
    return { ok: false, code: '' };
  }

  const code = queryParam(location, 'code');
  if (!code) {
    console.error(`${name} did not include an authorization code in ${location}`);
    return { ok: false, code: '' };
  }

  return { ok: true, code };
}

function exchangeOAuthToken(code) {
  const name = 'POST /oauth/token';
  const basicAuth = encoding.b64encode(`${OAUTH_CLIENT_ID}:${OAUTH_CLIENT_SECRET}`);
  const response = http.post(
    url('/oauth/token'),
    {
      grant_type: 'authorization_code',
      code,
      redirect_uri: OAUTH_REDIRECT_URI,
      client_id: OAUTH_CLIENT_ID,
      client_secret: OAUTH_CLIENT_SECRET,
    },
    requestParams(name, {
      headers: {
        Authorization: `Basic ${basicAuth}`,
        'Content-Type': 'application/x-www-form-urlencoded',
      },
    }),
  );
  addTiming(timings.oauthToken, response);
  const ok = expectStatus(response, 200, name);
  const payload = ok ? parseJSON(response, name) : null;
  return {
    ok: ok && Boolean(payload && payload.access_token),
    payload,
  };
}

function googleFulfillment(accessToken, intent, payload) {
  const metricByIntent = {
    'action.devices.SYNC': timings.googleSync,
    'action.devices.EXECUTE': timings.googleExecute,
    'action.devices.QUERY': timings.googleQuery,
    'action.devices.DISCONNECT': timings.googleDisconnect,
  };
  const labelByIntent = {
    'action.devices.SYNC': 'POST /api/google/fulfillment SYNC',
    'action.devices.EXECUTE': 'POST /api/google/fulfillment EXECUTE',
    'action.devices.QUERY': 'POST /api/google/fulfillment QUERY',
    'action.devices.DISCONNECT': 'POST /api/google/fulfillment DISCONNECT',
  };
  const label = labelByIntent[intent];
  const response = http.post(
    url('/api/google/fulfillment'),
    JSON.stringify({
      requestId: uniqueSuffix('request'),
      inputs: [{ intent, payload }],
    }),
    jsonParams(label, {
      headers: bearerHeaders(accessToken),
      tags: {
        intent,
      },
    }),
  );
  addTiming(metricByIntent[intent], response);
  const ok = expectStatus(response, 200, label);
  const parsedPayload = ok ? parseJSON(response, label) : null;
  return { ok, payload: parsedPayload };
}

function queryParam(location, name) {
  const match = location.match(new RegExp(`[?&]${name}=([^&]+)`));
  if (!match) {
    return '';
  }
  return decodeURIComponent(match[1]);
}

function syncResponseIncludesDevice(payload, deviceID) {
  return Boolean(
    payload &&
      payload.payload &&
      Array.isArray(payload.payload.devices) &&
      payload.payload.devices.some((device) => device.id === deviceID),
  );
}

function buildExecutePayload(deviceID, on) {
  return {
    commands: [
      {
        devices: [{ id: deviceID }],
        execution: [
          {
            command: 'action.devices.commands.OnOff',
            params: { on },
          },
        ],
      },
    ],
  };
}

function executeResponseSucceeded(payload) {
  return Boolean(
    payload &&
      payload.payload &&
      Array.isArray(payload.payload.commands) &&
      payload.payload.commands.some((command) => command.status === 'SUCCESS'),
  );
}

function queryResponseSucceeded(payload, deviceID) {
  const deviceState = payload && payload.payload && payload.payload.devices && payload.payload.devices[deviceID];
  return Boolean(deviceState && deviceState.status === 'SUCCESS');
}

function bestEffortCleanup(credentials, deviceID, homeID, adminSessionToken) {
  group('cleanup', () => {
    if (credentials && (deviceID || homeID)) {
      const cleanupLogin = loginSession(credentials.username, credentials.password);
      if (cleanupLogin.ok) {
        if (deviceID) {
          deleteDevice(cleanupLogin.sessionToken, deviceID);
        }
        if (homeID) {
          deleteHome(cleanupLogin.sessionToken, homeID);
        }
        logoutSession(cleanupLogin.sessionToken, true);
      }
    }
    if (adminSessionToken) {
      logoutSession(adminSessionToken, true);
    }
  });
}

export function setup() {
  if (!__ENV.K6_ADMIN_USERNAME || !__ENV.K6_ADMIN_PASSWORD) {
    throw new Error('K6_ADMIN_USERNAME and K6_ADMIN_PASSWORD must be set by tests/k6/run.sh');
  }
  if (!OAUTH_CLIENT_SECRET) {
    throw new Error('OAUTH_CLIENT_SECRET must be set to exercise /oauth/token');
  }

  const adminLogin = loginSession(__ENV.K6_ADMIN_USERNAME, __ENV.K6_ADMIN_PASSWORD);
  if (!adminLogin.ok) {
    throw new Error('failed to authenticate the admin stress-test user');
  }

  const products = listProducts(adminLogin.sessionToken);
  if (!products.ok) {
    throw new Error('failed to list products during setup');
  }
  const product = selectProduct(products.payload);

  const setupVersion = `setup_${TEST_RUN_ID}`.replace(/[^A-Za-z0-9._-]+/g, '_').slice(0, 60);
  const initialFirmware = uploadFirmware(adminLogin.sessionToken, Number(product.product_id), setupVersion);
  if (!initialFirmware.ok) {
    throw new Error('failed to upload initial firmware during setup');
  }

  const sharedHomeStartedAt = Date.now();
  const home = enrollHome(adminLogin.sessionToken, `oauth-${TEST_RUN_ID}`);
  if (!home.ok) {
    throw new Error('failed to create the shared OAuth home during setup');
  }
  const sharedHomeReady = waitForHomeReadyAfterDelay(
    adminLogin.sessionToken,
    home.homeID,
    'setup shared OAuth home',
    sharedHomeStartedAt,
  );
  if (!sharedHomeReady.ok) {
    throw new Error('shared OAuth home did not become ready asynchronously during setup');
  }

  const device = enrollDevice(
    adminLogin.sessionToken,
    home.homeID,
    Number(product.product_id),
    product.name,
    `oauth-${TEST_RUN_ID}`,
  );
  if (!device.ok) {
    throw new Error('failed to create the shared OAuth device during setup');
  }

  const auth = authorizeCode(adminLogin.sessionToken, uniqueSuffix('setup-state'));
  if (!auth.ok) {
    throw new Error('failed to authorize the shared OAuth token during setup');
  }
  const tokenResponse = exchangeOAuthToken(auth.code);
  if (!tokenResponse.ok) {
    throw new Error('failed to exchange the shared OAuth token during setup');
  }

  const deviceLoadCredentials = createUserCredentials('device-load');
  if (!registerUser(deviceLoadCredentials)) {
    throw new Error('failed to create the shared device-load user during setup');
  }
  const deviceLoadLogin = loginSession(deviceLoadCredentials.username, deviceLoadCredentials.password);
  if (!deviceLoadLogin.ok) {
    throw new Error('failed to authenticate the shared device-load user during setup');
  }

  const deviceLoadHomeStartedAt = Date.now();
  const deviceLoadHome = enrollHome(deviceLoadLogin.sessionToken, `device-load-${TEST_RUN_ID}`);
  if (!deviceLoadHome.ok) {
    throw new Error('failed to create the shared device-load home during setup');
  }
  const deviceLoadHomeReady = waitForHomeReadyAfterDelay(
    deviceLoadLogin.sessionToken,
    deviceLoadHome.homeID,
    'setup shared device-load home',
    deviceLoadHomeStartedAt,
  );
  if (!deviceLoadHomeReady.ok) {
    throw new Error('shared device-load home did not become ready asynchronously during setup');
  }

  logoutSession(adminLogin.sessionToken, true);

  return {
    adminUsername: __ENV.K6_ADMIN_USERNAME,
    adminPassword: __ENV.K6_ADMIN_PASSWORD,
    productID: Number(product.product_id),
    productName: product.name,
    sharedHomeID: home.homeID,
    sharedDeviceID: device.deviceID,
    sharedPowerID: `${device.deviceID}:power`,
    sharedAccessToken: tokenResponse.payload.access_token,
    deviceLoadUsername: deviceLoadCredentials.username,
    deviceLoadPassword: deviceLoadCredentials.password,
    deviceLoadSessionToken: deviceLoadLogin.sessionToken,
    deviceLoadHomeID: deviceLoadHome.homeID,
  };
}

export function userFlow(setupData) {
  const credentials = createUserCredentials('user');
  const runSuffix = uniqueSuffix('flow');
  const flowIteration = execution.scenario.iterationInTest;

  let userSessionToken = '';
  let adminSessionToken = '';
  let homeID = 0;
  let deviceID = 0;
  let accessToken = '';

  try {
    let shouldContinue = true;

    group('register', () => {
      shouldContinue = registerUser(credentials);
    });
    if (!shouldContinue) {
      return;
    }

    group('session', () => {
      const loginPage = loadLoginPage();
      shouldContinue = loginPage.ok;
      if (!shouldContinue) {
        return;
      }

      const formLogin = submitLoginForm(credentials.username, credentials.password);
      shouldContinue = formLogin.ok;
      if (!shouldContinue) {
        return;
      }

      const login = loginSession(credentials.username, credentials.password);
      shouldContinue = login.ok;
      if (!shouldContinue) {
        return;
      }

      userSessionToken = login.sessionToken;
      const meName = 'GET /api/session/me';
      const me = http.get(
        url('/api/session/me'),
        requestParams(meName, {
          cookies: sessionCookies(userSessionToken),
        }),
      );
      addTiming(timings.sessionMe, me);
      shouldContinue = expectStatus(me, 200, meName);
      if (!shouldContinue) {
        return;
      }

      const mePayload = parseJSON(me, meName);
      shouldContinue = expectCondition(
        mePayload && mePayload.username === credentials.username,
        'GET /api/session/me returned the logged-in user',
      );
    });
    if (!shouldContinue) {
      return;
    }

    group('enrollment', () => {
      const homeStartedAt = Date.now();
      const home = enrollHome(userSessionToken, runSuffix);
      shouldContinue = home.ok;
      if (!shouldContinue) {
        return;
      }
      homeID = home.homeID;

      const homes = listHomes(userSessionToken);
      shouldContinue = homes.ok && Array.isArray(homes.payload);
      if (!shouldContinue) {
        console.error('GET /api/enroll/homes did not return an array payload');
        return;
      }
      shouldContinue = expectCondition(
        homes.payload.some((homeItem) => Number(homeItem.home_id) === homeID),
        'GET /api/enroll/homes includes the created home',
      );
      if (!shouldContinue) {
        return;
      }

      const asyncReady = waitForHomeReadyAfterDelay(
        userSessionToken,
        homeID,
        `iteration home ${homeID}`,
        homeStartedAt,
      );
      shouldContinue = asyncReady.ok;
      if (!shouldContinue) {
        return;
      }

      const device = enrollDevice(
        userSessionToken,
        homeID,
        setupData.productID,
        setupData.productName,
        runSuffix,
      );
      shouldContinue = device.ok;
      if (!shouldContinue) {
        return;
      }
      deviceID = device.deviceID;

      const devices = listHomeDevices(userSessionToken, homeID);
      shouldContinue = devices.ok && Array.isArray(devices.payload);
      if (!shouldContinue) {
        console.error('GET /api/enroll/home/:homeID/devices did not return an array payload');
        return;
      }
      shouldContinue = expectCondition(
        devices.payload.some((deviceItem) => Number(deviceItem.device_id) === deviceID),
        'GET /api/enroll/home/:homeID/devices includes the created device',
      );
      if (!shouldContinue) {
        return;
      }

      const status = getDeviceStatus(userSessionToken, deviceID);
      shouldContinue = status.ok;
    });
    if (!shouldContinue) {
      return;
    }

    group('device_update', () => {
      const update = triggerDeviceUpdate(userSessionToken, deviceID);
      shouldContinue = update.ok;
    });
    if (!shouldContinue) {
      return;
    }

    group('admin', () => {
      const adminLogin = loginSession(setupData.adminUsername, setupData.adminPassword);
      shouldContinue = adminLogin.ok;
      if (!shouldContinue) {
        return;
      }
      adminSessionToken = adminLogin.sessionToken;

      const products = listProducts(adminSessionToken);
      shouldContinue = products.ok;
      if (!shouldContinue) {
        return;
      }

      const version = uniqueSuffix('firmware').replace(/[^A-Za-z0-9._-]+/g, '_').slice(0, 60);
      const upload = uploadFirmware(adminSessionToken, setupData.productID, version);
      shouldContinue = upload.ok;
      if (!shouldContinue) {
        return;
      }

      const rollout = rolloutFirmware(adminSessionToken, setupData.productID);
      shouldContinue = rollout.ok;
      if (!shouldContinue) {
        return;
      }

      const rollouts = listRollouts(adminSessionToken, setupData.productID);
      shouldContinue = rollouts.ok;
      if (!shouldContinue) {
        return;
      }

      shouldContinue = logoutSession(adminSessionToken);
      adminSessionToken = '';
    });
    if (!shouldContinue) {
      return;
    }

    group('oauth', () => {
      const adminLogin = loginSession(setupData.adminUsername, setupData.adminPassword);
      shouldContinue = adminLogin.ok;
      if (!shouldContinue) {
        return;
      }
      adminSessionToken = adminLogin.sessionToken;

      const auth = authorizeCode(adminSessionToken, uniqueSuffix('state'));
      shouldContinue = auth.ok;
      if (!shouldContinue) {
        return;
      }

      const tokenResponse = exchangeOAuthToken(auth.code);
      shouldContinue = tokenResponse.ok;
      if (!shouldContinue) {
        return;
      }
      accessToken = tokenResponse.payload.access_token;
    });
    if (!shouldContinue) {
      return;
    }

    group('fulfillment', () => {
      const sync = googleFulfillment(accessToken, 'action.devices.SYNC', {});
      shouldContinue = sync.ok;
      if (!shouldContinue) {
        return;
      }
      shouldContinue = expectCondition(
        syncResponseIncludesDevice(sync.payload, setupData.sharedPowerID),
        'SYNC response includes the shared device capability',
      );
      if (!shouldContinue) {
        return;
      }

      const execute = googleFulfillment(
        accessToken,
        'action.devices.EXECUTE',
        buildExecutePayload(setupData.sharedPowerID, flowIteration % 2 === 0),
      );
      shouldContinue = execute.ok;
      if (!shouldContinue) {
        return;
      }
      shouldContinue = expectCondition(
        executeResponseSucceeded(execute.payload),
        'EXECUTE response reports success',
      );
      if (!shouldContinue) {
        return;
      }

      const query = googleFulfillment(accessToken, 'action.devices.QUERY', {
        devices: [{ id: setupData.sharedPowerID }],
      });
      shouldContinue = query.ok;
      if (!shouldContinue) {
        return;
      }
      shouldContinue = expectCondition(
        queryResponseSucceeded(query.payload, setupData.sharedPowerID),
        'QUERY response reports success for the shared device capability',
      );
      if (!shouldContinue) {
        return;
      }

      googleFulfillment(accessToken, 'action.devices.DISCONNECT', {});
    });
  } finally {
    bestEffortCleanup(credentials, deviceID, homeID, adminSessionToken);
  }
}

export function asyncHomeBurst(setupData) {
  const credentials = createUserCredentials('burst');
  const runSuffix = uniqueSuffix('burst');
  let sessionToken = '';
  let homeID = 0;

  try {
    let shouldContinue = true;

    group('async_home_burst_register', () => {
      shouldContinue = registerUser(credentials);
    });
    if (!shouldContinue) {
      return;
    }

    const login = loginSession(credentials.username, credentials.password);
    shouldContinue = login.ok;
    if (!shouldContinue) {
      return;
    }
    sessionToken = login.sessionToken;

    group('async_home_burst', () => {
      const homeStartedAt = Date.now();
      const home = enrollHome(sessionToken, runSuffix);
      shouldContinue = home.ok;
      if (!shouldContinue) {
        return;
      }
      homeID = home.homeID;

      const homes = listHomes(sessionToken);
      shouldContinue = homes.ok && Array.isArray(homes.payload);
      if (!shouldContinue) {
        console.error('GET /api/enroll/homes did not return an array payload for the async burst');
        return;
      }
      shouldContinue = expectCondition(
        homes.payload.some((homeItem) => Number(homeItem.home_id) === homeID),
        'GET /api/enroll/homes includes the async burst home',
      );
      if (!shouldContinue) {
        return;
      }

      const asyncReady = waitForHomeReadyAfterDelay(
        sessionToken,
        homeID,
        `burst home ${homeID}`,
        homeStartedAt,
      );
      shouldContinue = asyncReady.ok;
      if (!shouldContinue) {
        return;
      }

      shouldContinue = deleteHome(sessionToken, homeID);
      if (!shouldContinue) {
        return;
      }
      homeID = 0;
    });
  } finally {
    if (sessionToken && homeID) {
      deleteHome(sessionToken, homeID);
    }
    if (sessionToken) {
      logoutSession(sessionToken, true);
    }
  }
}

export function fulfillmentParallel(setupData) {
  if (!setupData.sharedAccessToken) {
    throw new Error('setup did not return a shared access token for fulfillment load');
  }

  group('fulfillment_parallel', () => {
    const iteration = execution.scenario.iterationInTest;
    const mode = iteration % 3;

    if (mode === 0) {
      const sync = googleFulfillment(setupData.sharedAccessToken, 'action.devices.SYNC', {});
      if (!sync.ok) {
        return;
      }
      expectCondition(
        syncResponseIncludesDevice(sync.payload, setupData.sharedPowerID),
        'Parallel SYNC response includes the shared device capability',
      );
      return;
    }

    if (mode === 1) {
      const query = googleFulfillment(setupData.sharedAccessToken, 'action.devices.QUERY', {
        devices: [{ id: setupData.sharedPowerID }],
      });
      if (!query.ok) {
        return;
      }
      expectCondition(
        queryResponseSucceeded(query.payload, setupData.sharedPowerID),
        'Parallel QUERY response reports success for the shared device capability',
      );
      return;
    }

    const execute = googleFulfillment(
      setupData.sharedAccessToken,
      'action.devices.EXECUTE',
      buildExecutePayload(setupData.sharedPowerID, iteration % 2 === 0),
    );
    if (!execute.ok) {
      return;
    }
    expectCondition(
      executeResponseSucceeded(execute.payload),
      'Parallel EXECUTE response reports success',
    );
  });
}

export function deviceEnrollStream(setupData) {
  if (!setupData.deviceLoadSessionToken || !setupData.deviceLoadHomeID) {
    throw new Error('setup did not return a shared device-load session and home for device stream load');
  }

  group('device_stream_enroll', () => {
    const iteration = execution.scenario.iterationInTest;
    const deviceName = deviceStreamDeviceName(iteration);
    const device = enrollNamedDevice(
      setupData.deviceLoadSessionToken,
      setupData.deviceLoadHomeID,
      setupData.productID,
      setupData.productName,
      deviceName,
    );
    if (device.ok) {
      expectCondition(device.deviceID > 0, 'Device stream enrollment returned a device id');
    }
    paceDeviceStream(DEVICE_STREAM_ITERATIONS);
  });
}

export function deviceCleanupStream(setupData) {
  if (!setupData.deviceLoadSessionToken || !setupData.deviceLoadHomeID) {
    throw new Error('setup did not return a shared device-load session and home for device cleanup load');
  }

  group('device_stream_cleanup', () => {
    const iteration = execution.scenario.iterationInTest;
    const deviceName = deviceStreamDeviceName(iteration);
    const deleted = deleteDeviceByNameWithRetry(
      setupData.deviceLoadSessionToken,
      setupData.deviceLoadHomeID,
      deviceName,
    );
    const found = expectCondition(
      deleted.found,
      `Device stream cleanup found ${deviceName}`,
    );
    if (found) {
      expectCondition(deleted.ok, `Device stream cleanup deleted ${deviceName}`);
    }
    paceDeviceStream(DEVICE_STREAM_ITERATIONS);
  });
}

export default function (setupData) {
  return userFlow(setupData);
}

export function teardown(setupData) {
  const adminLogin = loginSession(setupData.adminUsername, setupData.adminPassword);
  if (!adminLogin.ok) {
    return;
  }

  let deviceLoadSessionToken = setupData.deviceLoadSessionToken || '';
  if (!deviceLoadSessionToken && setupData.deviceLoadUsername && setupData.deviceLoadPassword) {
    const deviceLoadLogin = loginSession(setupData.deviceLoadUsername, setupData.deviceLoadPassword);
    if (deviceLoadLogin.ok) {
      deviceLoadSessionToken = deviceLoadLogin.sessionToken;
    }
  }

  if (setupData.sharedDeviceID) {
    deleteDevice(adminLogin.sessionToken, setupData.sharedDeviceID);
  }
  if (setupData.sharedHomeID) {
    deleteHome(adminLogin.sessionToken, setupData.sharedHomeID);
  }
  if (setupData.deviceLoadHomeID && deviceLoadSessionToken) {
    const deleted = deleteHome(deviceLoadSessionToken, setupData.deviceLoadHomeID);
    if (
      !deleted
      && setupData.deviceLoadUsername
      && setupData.deviceLoadPassword
    ) {
      const retryLogin = loginSession(setupData.deviceLoadUsername, setupData.deviceLoadPassword);
      if (retryLogin.ok) {
        deviceLoadSessionToken = retryLogin.sessionToken;
        deleteHome(deviceLoadSessionToken, setupData.deviceLoadHomeID);
      }
    }
  }
  if (deviceLoadSessionToken) {
    logoutSession(deviceLoadSessionToken, true);
  }
  logoutSession(adminLogin.sessionToken, true);
}

export function handleSummary(data) {
  return {
    stdout: textSummary(data, {
      indent: ' ',
      enableColors: true,
    }),
    [SUMMARY_PATH]: JSON.stringify(data, null, 2),
  };
}
