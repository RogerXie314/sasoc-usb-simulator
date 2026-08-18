import requests, urllib3
urllib3.disable_warnings()
s = requests.Session(); s.verify = False
base = 'https://192.168.123.24:8440'

# Step 1: get JSESSIONID from homepage
r = s.get(base + '/', headers={'User-Agent': 'Mozilla/5.0'}, timeout=15)
print('Step 1 JSESSIONID:', s.cookies.get('JSESSIONID', 'none'))

# Step 2: try login/userLogin
r = s.post(base + '/login/userLogin', json={'userName':'xrg','password':'Admin@11'}, headers={
    'User-Agent': 'Mozilla/5.0', 'Content-Type': 'application/json'
}, timeout=15)
print('Step 2 status:', r.status_code)
print('Step 2 body:', r.text[:500])
print('Step 2 JSESSIONID:', s.cookies.get('JSESSIONID', 'none'))

# Step 3: if login succeeded, get user info to verify
if r.status_code == 200:
    r2 = s.get(base + '/login/getUserInfo', headers={'User-Agent': 'Mozilla/5.0'}, timeout=15)
    print('Step 3 getUserInfo:', r2.status_code, r2.text[:200])