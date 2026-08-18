import requests, urllib3
urllib3.disable_warnings()
s = requests.Session(); s.verify = False
base = 'https://192.168.123.24:8440'
r = s.get(base + '/static/js/app.a59fb4ce3f7fe0c1cd4f.js', headers={'User-Agent':'Mozilla/5.0'}, timeout=30)
text = r.text

# search for login-related action patterns
keywords = ['login/doLogin', 'login/submit', 'login/check', 'login/authenticate', 'user/login', 'singleLogin', 'loginAction', 'submitLogin', 'LoginByUsername', 'loginByPwd']
for kw in keywords:
    idx = 0
    count = 0
    while idx >= 0 and count < 10:
        idx = text.find(kw, idx)
        if idx < 0: break
        ctx = text[max(0,idx-100):min(len(text),idx+200)]
        if any(k in ctx.lower() for k in ['post', 'ajax', 'action', 'submit', 'this.', 'store', 'dispatch', 'data:', 'params']):
            print(f'--- {kw} at {idx} ---')
            print(ctx)
            print()
            count += 1
        idx += 1