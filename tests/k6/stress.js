import http from 'k6/http';
import encoding from 'k6/encoding';
import { check, group } from 'k6';
import { Trend } from 'k6/metrics';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.0.4/index.js';

const BASE_URL = stripTrailingSlash(__ENV.BASE_URL || 'http://localhost:8080');
const SESSION_COOKIE_NAME = __ENV.SESSION_COOKIE_NAME || 'user_session';
const OAUTH_CLIENT_ID = __ENV.OAUTH_CLIENT_ID || 'google-client';
const OAUTH_CLIENT_SECRET = __ENV.OAUTH_CLIENT_SECRET || '';
const OAUTH_REDIRECT_URI = __ENV.OAUTH_REDIRECT_URI || 'https://oauth-redirect.googleusercontent.com/';
const PRODUCT_NAME = __ENV.K6_PRODUCT_NAME || 'ml-smart-sensor-v1';
const DEFAULT_PRODUCT_ID = Number(__ENV.K6_PRODUCT_ID || '1');
const DEVICE_PUBLIC_KEY = __ENV.K6_DEVICE_PUBLIC_KEY || 'AQbnaqQshSiDwqVRxeH8lTij1x49dJjzhQqAwtbW4EI=';
const DUMMY_FIRMWARE = open('./testdata/dummy.bin', 'b');
const SUMMARY_PATH = __ENV.K6_SUMMARY_PATH || './artifacts/k6-summary.json';
const TEST_RUN_ID = __ENV.K6_RUN_ID || `${Date.now()}`;
const TARGET_VUS = Number(__ENV.K6_VUS || '10');
const HOLD_DURATION = __ENV.K6_DURATION || '1m';
const RAMP_UP_DURATION = __ENV.K6_RAMP_UP || '15s';
const RAMP_DOWN_DURATION = __ENV.K6_RAMP_DOWN || '10s';

const timings = {
  enrollUser: new Trend('api_enroll_user_duration', true),
  loginPage: new Trend('login_page_duration', true),
  formLogin: new Trend('login_form_duration', true),
  sessionLogin: new Trend('api_session_login_duration', true),
  sessionMe: new Trend('api_session_me_duration', true),
  sessionLogout: new Trend('api_session_logout_duration', true),
  enrollHome: new Trend('api_enroll_home_duration', true),
  listHomes: new Trend('api_enroll_homes_duration', true),
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

export const options = {
  insecureSkipTLSVerify: true,
  stages: [
    { duration: RAMP_UP_DURATION, target: TARGET_VUS },
    { duration: HOLD_DURATION, target: TARGET_VUS },
    { duration: RAMP_DOWN_DURATION, target: 0 },
  ],
  thresholds: {
    http_req_failed: ['rate<0.10'],
    http_req_duration: ['p(95)<2000'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
};

function stripTrailingSlash(value) {
  return value.replace(/\/+$/, '');
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

function logoutSession(sessionToken) {
  const name = 'POST /api/session/logout';
  const response = http.post(
    url('/api/session/logout'),
    null,
    requestParams(name, {
      cookies: sessionCookies(sessionToken),
    }),
  );
  addTiming(timings.sessionLogout, response);
  return expectStatus(response, 200, name);
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

function enrollDevice(sessionToken, homeID, productID, productName, nameSuffix) {
  const name = 'POST /api/enroll/device';
  const response = http.post(
    url('/api/enroll/device'),
    JSON.stringify({
      home_id: homeID,
      name: `k6-device-${nameSuffix}`,
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
        logoutSession(cleanupLogin.sessionToken);
      }
    }
    if (adminSessionToken) {
      logoutSession(adminSessionToken);
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

  const home = enrollHome(adminLogin.sessionToken, `oauth-${TEST_RUN_ID}`);
  if (!home.ok) {
    throw new Error('failed to create the shared OAuth home during setup');
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

  logoutSession(adminLogin.sessionToken);

  return {
    adminUsername: __ENV.K6_ADMIN_USERNAME,
    adminPassword: __ENV.K6_ADMIN_PASSWORD,
    productID: Number(product.product_id),
    productName: product.name,
    sharedHomeID: home.homeID,
    sharedDeviceID: device.deviceID,
    sharedPowerID: `${device.deviceID}:power`,
  };
}

export default function (setupData) {
  const credentials = createUserCredentials('user');
  const runSuffix = uniqueSuffix('flow');

  let userSessionToken = '';
  let adminSessionToken = '';
  let homeID = 0;
  let deviceID = 0;
  let accessToken = '';

  try {
    let shouldContinue = true;

    group('register', () => {
      const name = 'POST /api/enroll/user';
      const response = http.post(
        url('/api/enroll/user'),
        JSON.stringify(credentials),
        jsonParams(name),
      );
      addTiming(timings.enrollUser, response);
      shouldContinue = expectStatus(response, 201, name);
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
        sync.payload &&
          sync.payload.payload &&
          Array.isArray(sync.payload.payload.devices) &&
          sync.payload.payload.devices.some((device) => device.id === setupData.sharedPowerID),
        'SYNC response includes the shared device capability',
      );
      if (!shouldContinue) {
        return;
      }

      const execute = googleFulfillment(accessToken, 'action.devices.EXECUTE', {
        commands: [
          {
            devices: [{ id: setupData.sharedPowerID }],
            execution: [
              {
                command: 'action.devices.commands.OnOff',
                params: {
                  on: __ITER % 2 === 0,
                },
              },
            ],
          },
        ],
      });
      shouldContinue = execute.ok;
      if (!shouldContinue) {
        return;
      }
      shouldContinue = expectCondition(
        execute.payload &&
          execute.payload.payload &&
          Array.isArray(execute.payload.payload.commands) &&
          execute.payload.payload.commands.some((command) => command.status === 'SUCCESS'),
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
      const deviceState =
        query.payload &&
        query.payload.payload &&
        query.payload.payload.devices &&
        query.payload.payload.devices[setupData.sharedPowerID];
      shouldContinue = expectCondition(
        deviceState && deviceState.status === 'SUCCESS',
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

export function teardown(setupData) {
  const adminLogin = loginSession(setupData.adminUsername, setupData.adminPassword);
  if (!adminLogin.ok) {
    return;
  }

  if (setupData.sharedDeviceID) {
    deleteDevice(adminLogin.sessionToken, setupData.sharedDeviceID);
  }
  if (setupData.sharedHomeID) {
    deleteHome(adminLogin.sessionToken, setupData.sharedHomeID);
  }
  logoutSession(adminLogin.sessionToken);
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
