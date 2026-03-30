UPDATE oauth_clients
SET secret = ''
WHERE secret = 'my-secret-key';
