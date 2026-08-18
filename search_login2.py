import requests, urllib3, re
urllib3.disable_warnings()
s = requests.Session(); s.verify = False
base = 'https://192.168.123.24:8440'

# Get the main page HTML
r = s.get(base + '/', headers={'User-Agent': 'Mozilla/5.0'}, timeout=15)
html = r.text.encode('utf-8', errors='ignore').decode('utf-8', errors='ignore')

# Find all script tags
scripts = re.findall(r'<script[^>]*>', html)
for sc in scripts:
    print('script:', sc[:150])

# Find all link/dynamic imports
print('\n--- chunk patterns ---')
for pat in ['js/chunk','static/js','manifest','vendor','app']:
    idx = html.find(pat)
    if idx >= 0:
        print(f'[{pat}]: {html[max(0,idx-30):idx+100]}')

# Also try to get the login page HTML
r2 = s.get(base + '/login', headers={'User-Agent': 'Mozilla/5.0'}, timeout=15)
print('\n/login status:', r2.status_code, 'len:', len(r2.text))
# look for redirect
for h in ['Location', 'location']:
    print(f'{h}:', r2.headers.get(h, ''))

# Try /#/login
r3 = s.get(base + '/#/login', headers={'User-Agent': 'Mozilla/5.0'}, timeout=15)
print('/#/login status:', r3.status_code, 'len:', len(r3.text))