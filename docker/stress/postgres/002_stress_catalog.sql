INSERT INTO capabilities (component, trait_type, writable, google_device_type) VALUES
    ('switch', 'OnOff', true, 'action.devices.types.SWITCH'),
    ('binary_sensor', 'MotionDetection', false, 'action.devices.types.SENSOR'),
    ('light', 'Brightness', true, 'action.devices.types.LIGHT'),
    ('climate', 'TemperatureSetting', true, 'action.devices.types.THERMOSTAT'),
    ('fan', 'FanSpeed', true, 'action.devices.types.FAN'),
    ('humidifier', 'HumiditySetting', true, 'action.devices.types.HUMIDIFIER'),
    ('cover', 'OpenClose', true, 'action.devices.types.BLINDS'),
    ('lock', 'LockUnlock', true, 'action.devices.types.LOCK'),
    ('switch', 'StartStop', true, 'action.devices.types.WASHER'),
    ('scene', 'Scene', true, 'action.devices.types.SCENE')
ON CONFLICT (component, trait_type) DO UPDATE SET
    writable = EXCLUDED.writable,
    google_device_type = EXCLUDED.google_device_type;

INSERT INTO products (name) VALUES
    ('stress-product-01'),
    ('stress-product-02'),
    ('stress-product-03'),
    ('stress-product-04'),
    ('stress-product-05'),
    ('stress-product-06'),
    ('stress-product-07'),
    ('stress-product-08'),
    ('stress-product-09'),
    ('stress-product-10'),
    ('stress-product-11'),
    ('stress-product-12'),
    ('stress-product-13'),
    ('stress-product-14'),
    ('stress-product-15'),
    ('stress-product-16'),
    ('stress-product-17'),
    ('stress-product-18'),
    ('stress-product-19'),
    ('stress-product-20')
ON CONFLICT (name) DO NOTHING;

WITH product_capability_seed(product_name, component, trait_type, esphome_key) AS (
    VALUES
        ('stress-product-01', 'switch', 'OnOff', 'power'),
        ('stress-product-01', 'binary_sensor', 'MotionDetection', 'motion'),
        ('stress-product-01', 'light', 'Brightness', 'brightness'),
        ('stress-product-01', 'lock', 'LockUnlock', 'door_lock'),

        ('stress-product-02', 'switch', 'OnOff', 'power'),
        ('stress-product-02', 'climate', 'TemperatureSetting', 'temp_setpoint'),
        ('stress-product-02', 'fan', 'FanSpeed', 'fan_speed'),
        ('stress-product-02', 'cover', 'OpenClose', 'cover'),

        ('stress-product-03', 'switch', 'OnOff', 'power'),
        ('stress-product-03', 'humidifier', 'HumiditySetting', 'humidity_setpoint'),
        ('stress-product-03', 'scene', 'Scene', 'scene_select'),
        ('stress-product-03', 'switch', 'StartStop', 'run_cycle'),

        ('stress-product-04', 'switch', 'OnOff', 'power'),
        ('stress-product-04', 'binary_sensor', 'MotionDetection', 'motion'),
        ('stress-product-04', 'climate', 'TemperatureSetting', 'temp_setpoint'),
        ('stress-product-04', 'humidifier', 'HumiditySetting', 'humidity_setpoint'),

        ('stress-product-05', 'switch', 'OnOff', 'power'),
        ('stress-product-05', 'light', 'Brightness', 'brightness'),
        ('stress-product-05', 'fan', 'FanSpeed', 'fan_speed'),
        ('stress-product-05', 'lock', 'LockUnlock', 'door_lock'),

        ('stress-product-06', 'switch', 'OnOff', 'power'),
        ('stress-product-06', 'cover', 'OpenClose', 'cover'),
        ('stress-product-06', 'scene', 'Scene', 'scene_select'),
        ('stress-product-06', 'switch', 'StartStop', 'run_cycle'),

        ('stress-product-07', 'switch', 'OnOff', 'power'),
        ('stress-product-07', 'binary_sensor', 'MotionDetection', 'motion'),
        ('stress-product-07', 'fan', 'FanSpeed', 'fan_speed'),
        ('stress-product-07', 'scene', 'Scene', 'scene_select'),

        ('stress-product-08', 'switch', 'OnOff', 'power'),
        ('stress-product-08', 'light', 'Brightness', 'brightness'),
        ('stress-product-08', 'climate', 'TemperatureSetting', 'temp_setpoint'),
        ('stress-product-08', 'cover', 'OpenClose', 'cover'),

        ('stress-product-09', 'switch', 'OnOff', 'power'),
        ('stress-product-09', 'humidifier', 'HumiditySetting', 'humidity_setpoint'),
        ('stress-product-09', 'lock', 'LockUnlock', 'door_lock'),
        ('stress-product-09', 'switch', 'StartStop', 'run_cycle'),

        ('stress-product-10', 'switch', 'OnOff', 'power'),
        ('stress-product-10', 'binary_sensor', 'MotionDetection', 'motion'),
        ('stress-product-10', 'light', 'Brightness', 'brightness'),
        ('stress-product-10', 'fan', 'FanSpeed', 'fan_speed'),

        ('stress-product-11', 'switch', 'OnOff', 'power'),
        ('stress-product-11', 'climate', 'TemperatureSetting', 'temp_setpoint'),
        ('stress-product-11', 'humidifier', 'HumiditySetting', 'humidity_setpoint'),
        ('stress-product-11', 'scene', 'Scene', 'scene_select'),

        ('stress-product-12', 'switch', 'OnOff', 'power'),
        ('stress-product-12', 'cover', 'OpenClose', 'cover'),
        ('stress-product-12', 'lock', 'LockUnlock', 'door_lock'),
        ('stress-product-12', 'switch', 'StartStop', 'run_cycle'),

        ('stress-product-13', 'switch', 'OnOff', 'power'),
        ('stress-product-13', 'binary_sensor', 'MotionDetection', 'motion'),
        ('stress-product-13', 'climate', 'TemperatureSetting', 'temp_setpoint'),
        ('stress-product-13', 'lock', 'LockUnlock', 'door_lock'),

        ('stress-product-14', 'switch', 'OnOff', 'power'),
        ('stress-product-14', 'light', 'Brightness', 'brightness'),
        ('stress-product-14', 'humidifier', 'HumiditySetting', 'humidity_setpoint'),
        ('stress-product-14', 'cover', 'OpenClose', 'cover'),

        ('stress-product-15', 'switch', 'OnOff', 'power'),
        ('stress-product-15', 'fan', 'FanSpeed', 'fan_speed'),
        ('stress-product-15', 'scene', 'Scene', 'scene_select'),
        ('stress-product-15', 'switch', 'StartStop', 'run_cycle'),

        ('stress-product-16', 'switch', 'OnOff', 'power'),
        ('stress-product-16', 'binary_sensor', 'MotionDetection', 'motion'),
        ('stress-product-16', 'scene', 'Scene', 'scene_select'),
        ('stress-product-16', 'cover', 'OpenClose', 'cover'),

        ('stress-product-17', 'switch', 'OnOff', 'power'),
        ('stress-product-17', 'light', 'Brightness', 'brightness'),
        ('stress-product-17', 'lock', 'LockUnlock', 'door_lock'),
        ('stress-product-17', 'humidifier', 'HumiditySetting', 'humidity_setpoint'),

        ('stress-product-18', 'switch', 'OnOff', 'power'),
        ('stress-product-18', 'climate', 'TemperatureSetting', 'temp_setpoint'),
        ('stress-product-18', 'fan', 'FanSpeed', 'fan_speed'),
        ('stress-product-18', 'switch', 'StartStop', 'run_cycle'),

        ('stress-product-19', 'switch', 'OnOff', 'power'),
        ('stress-product-19', 'binary_sensor', 'MotionDetection', 'motion'),
        ('stress-product-19', 'light', 'Brightness', 'brightness'),
        ('stress-product-19', 'humidifier', 'HumiditySetting', 'humidity_setpoint'),

        ('stress-product-20', 'switch', 'OnOff', 'power'),
        ('stress-product-20', 'fan', 'FanSpeed', 'fan_speed'),
        ('stress-product-20', 'lock', 'LockUnlock', 'door_lock'),
        ('stress-product-20', 'scene', 'Scene', 'scene_select')
)
INSERT INTO product_capabilities (product_id, capability_id, esphome_key)
SELECT p.id, c.id, seed.esphome_key
FROM product_capability_seed AS seed
JOIN products AS p ON p.name = seed.product_name
JOIN capabilities AS c
    ON c.component = seed.component
   AND c.trait_type = seed.trait_type
ON CONFLICT (product_id, capability_id, esphome_key) DO NOTHING;
