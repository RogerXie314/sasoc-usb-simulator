import requests, urllib3, json
urllib3.disable_warnings()
s = requests.Session()
s.verify = False
base = 'https://192.168.123.24:8440'

# 先访问首页拿登录页面信息
r = s.get(base + '/', timeout=15)
print(f'GET /: {r.status_code}, Location: {r.headers.get("Location","")}')

# 常见登录接口
tests = [
    ('POST', '/v2/login/doLogin', {'username':'xrg','password':'Admin@11'}),
    ('POST', '/v2/login/check', {'username':'xrg','password':'Admin@11'}),
    ('POST', '/v2/login/auth', {'username':'xrg','password':'Admin@11'}),
    ('POST', '/v2/login/getUserInfo', {}),
    ('POST', '/USM/login', {'username':'xrg','password':'Admin@11'}),
    ('POST', '/USM/login/doLogin', {'username':'xrg','password':'Admin@11'}),
]
for method, path, data in tests:
    try:
        r = s.post(base + path, json=data, timeout=15)
        print(f'{method} {path}: {r.status_code} {r.text[:200]}')
    except Exception as e:
        print(f'{method} {path}: ERROR {e}')