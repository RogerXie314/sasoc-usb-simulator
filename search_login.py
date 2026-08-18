import requests, urllib3, re
urllib3.disable_warnings()
s = requests.Session(); s.verify = False
base = 'https://192.168.123.24:8440'

# Get the main SPA page
r = s.get(base + '/', headers={'User-Agent': 'Mozilla/5.0'}, timeout=15)
js_files = re.findall(r'src="([^"]+\.js[^"]*)"', r.text)
print('JS files:', js_files[:15])

# Search each JS bundle for login-related API endpoints
keywords = ['login', 'Login', 'authenticate', 'auth/login', 'token', 'accessToken', 'getUserInfo', '/v2/']
for f in js_files[:20]:
    if not f.startswith('http'):
        url = base + (f if f.startswith('/') else '/' + f)
    else:
        url = f
    try:
        r = s.get(url, headers={'User-Agent': 'Mozilla/5.0'}, timeout=15)
        if r.status_code == 200 and len(r.text) > 1000:
            for kw in keywords:
                idx = r.text.find(kw)
                if idx >= 0:
                    ctx = r.text[max(0,idx-60):min(len(r.text),idx+120)]
                    if any(k in ctx.lower() for k in ['login', 'auth', 'token', 'password', 'credential']):
                        print(f'[{f}] [{kw}]: {ctx}')
                        break
    except Exception as e:
        pass