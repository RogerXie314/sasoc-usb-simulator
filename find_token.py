import requests, urllib3
urllib3.disable_warnings()
s = requests.Session(); s.verify = False
base = 'https://192.168.123.24:8440'
r = s.get(base + '/static/js/app.a59fb4ce3f7fe0c1cd4f.js', headers={'User-Agent':'Mozilla/5.0'}, timeout=30)
text = r.text

# find userLogin login success handler
for kw in ['userLogin', 'setItem(', 'accessToken', 'sessionStorage.setItem', 'loginSuccess', 'JSESSIONID']:
    idx = text.find(kw)
    if idx >= 0:
        ctx = text[max(0,idx-300):min(len(text),idx+300)]
        print(f'--- {kw} ---')
        print(ctx)
        print()