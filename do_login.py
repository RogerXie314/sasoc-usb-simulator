import requests, urllib3, base64, hashlib
from Crypto.Cipher import AES
urllib3.disable_warnings()

KEY = b'scmZUakaZiYf8Tp5'

def encrypt_cryptojs(plaintext, key):
    """CryptoJS.AES.encrypt 兼容实现"""
    salt = b'\x01\x02\x03\x04\x05\x06\x07\x08'
    d = b''; data = b''
    while len(d) < 48:
        data = hashlib.md5(data + key + salt).digest()
        d += data
    derived_key = d[:32]
    iv = d[32:48]
    pad_len = 16 - (len(plaintext) % 16)
    padded = plaintext + chr(pad_len) * pad_len
    cipher = AES.new(derived_key, AES.MODE_CBC, iv)
    encrypted = cipher.encrypt(padded.encode())
    return base64.b64encode(b'Salted__' + salt + encrypted).decode()

enc_uname = encrypt_cryptojs('xrg', KEY)
enc_pwd = encrypt_cryptojs('Admin@11', KEY)
print(f'uname: {enc_uname[:60]}...')
print(f'pwd: {enc_pwd[:60]}...')

s = requests.Session(); s.verify = False
r = s.post('https://192.168.123.24:8440/USM/login.do', data={
    'uname': enc_uname, 'pwd': enc_pwd,
    'isUkeyUser': 'false', 'language': 'zh'
}, headers={'User-Agent': 'Mozilla/5.0'}, timeout=15)
print(f'登录: {r.status_code} {r.text[:300]}')
print(f'Cookie: {dict(s.cookies)}')