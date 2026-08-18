"""登录SASOC平台，获取JSESSIONID和Authorization Token"""
import requests, urllib3, base64, hashlib, os, json
from Crypto.Cipher import AES
urllib3.disable_warnings()

BASE = 'https://192.168.123.24:8440'
USERNAME = 'xrg'
PASSWORD = 'Admin@11'
KEY = 'scmZUakaZiYf8Tp5'  # 前端login.js中的加密密钥

def encrypt_cryptojs(plaintext):
    """CryptoJS.AES.encrypt 兼容实现：随机salt + EVP_BytesToKey"""
    salt = os.urandom(8)
    d = b''; data = b''
    while len(d) < 48:
        data = hashlib.md5(data + KEY.encode() + salt).digest()
        d += data
    key = d[:32]; iv = d[32:48]
    # PKCS7 padding
    pad_len = 16 - (len(plaintext) % 16)
    padded = plaintext + chr(pad_len) * pad_len
    cipher = AES.new(key, AES.MODE_CBC, iv)
    encrypted = cipher.encrypt(padded.encode('utf-8'))
    return base64.b64encode(b'Salted__' + salt + encrypted).decode()

s = requests.Session()
s.verify = False

# Step 1: 访问首页获取初始JSESSIONID
print('Step 1: 访问首页...')
r = s.get(BASE + '/', timeout=15, allow_redirects=True)
print(f'  状态: {r.status_code}, URL: {r.url}')
print(f'  Cookie: {dict(s.cookies)}')

# Step 2: 尝试多种登录端点
login_endpoints = [
    '/USM/login.do',
    '/USM/ieg/login',
    '/v2/login',
]

enc_uname = encrypt_cryptojs(USERNAME)
enc_pwd = encrypt_cryptojs(PASSWORD)

for endpoint in login_endpoints:
    print(f'\nStep 2: 尝试 {endpoint}...')
    # 用 form 格式
    try:
        r = s.post(BASE + endpoint, data={
            'uname': enc_uname,
            'pwd': enc_pwd,
            'isUkeyUser': 'false',
            'language': 'zh'
        }, headers={
            'User-Agent': 'Mozilla/5.0',
            'Content-Type': 'application/x-www-form-urlencoded'
        }, timeout=15, allow_redirects=True)
        print(f'  状态: {r.status_code}, 长度: {len(r.text)}')
        if r.status_code == 200 and len(r.text) < 100:
            print(f'  响应: {r.text}')
        elif r.status_code != 404:
            print(f'  响应: {r.text[:300]}')
        print(f'  Cookie: {dict(s.cookies)}')
        
        # 如果登录成功，继续验证
        if r.status_code == 200 and ('yes' in r.text.lower() or 'success' in r.text.lower()):
            break
    except Exception as e:
        print(f'  错误: {e}')

# Step 3: 尝试直接访问 /USM/ieg/usbApply 看是否已登录
print('\nStep 3: 验证登录状态...')
r = s.get(BASE + '/USM/ieg/usbApply/list', timeout=15, headers={
    'User-Agent': 'Mozilla/5.0',
    'Accept': 'application/json'
})
print(f'  状态: {r.status_code}')
print(f'  响应: {r.text[:300]}')
print(f'  最终 Cookie: {dict(s.cookies)}')