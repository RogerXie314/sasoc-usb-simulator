import requests, urllib3, re
urllib3.disable_warnings()
s = requests.Session()
s.verify = False
base = 'https://192.168.123.24:8440'
r = s.get(base + '/USM/jsp/login/login.jsp', timeout=15)
print(f'状态: {r.status_code}, 长度: {len(r.text)}')

# 搜 login URL 和 API 路径
patterns = ['action=', 'url:', 'loginUrl', 'login_url', 'apiUrl', '/login', '/auth', 'doLogin', 'submit', 'token', 'accessToken']
for pat in patterns:
    idx = r.text.lower().find(pat.lower())
    if idx >= 0:
        print(f'  [{pat}]: {r.text[max(0,idx-50):idx+120]}...')

# 找所有 script 标签
import re as re2
scripts = re2.findall(r'src="([^"]+)"', r.text)
for s in scripts:
    if s.endswith('.js') or 'login' in s.lower():
        print(f'  script: {s}')