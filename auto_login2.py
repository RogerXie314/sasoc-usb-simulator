"""完整自动化登录 + 批量生成申领码"""
import requests, urllib3, base64, json, time, threading, os
from cryptography.hazmat.primitives import serialization, hashes
from cryptography.hazmat.primitives.asymmetric import padding
urllib3.disable_warnings()

BASE = 'https://192.168.123.24:8440'
USERNAME = 'xrg'
PASSWORD = 'Admin@11'

def rsa_encrypt(plaintext, pubkey_pem):
    """RSA PKCS1v15 加密"""
    pubkey = serialization.load_pem_public_key(pubkey_pem.encode())
    encrypted = pubkey.encrypt(plaintext.encode(), padding.PKCS1v15())
    return base64.b64encode(encrypted).decode()

# Step 1: 获取公钥
print('Step 1: 获取公钥...')
s = requests.Session(); s.verify = False
r = s.get(BASE + '/login/getPublicKey', headers={'User-Agent': 'Mozilla/5.0'}, timeout=15)
pubkey = '-----BEGIN PUBLIC KEY-----\n' + r.json()['message'] + '\n-----END PUBLIC KEY-----'
print(f'  公钥长度: {len(pubkey)}')

# Step 2: RSA加密密码并登录
print('Step 2: 登录...')
enc_pwd = rsa_encrypt(PASSWORD, pubkey)
r = s.post(BASE + '/login/userLogin', json={'userName': USERNAME, 'userPassword': enc_pwd},
           headers={'User-Agent': 'Mozilla/5.0', 'Content-Type': 'application/json'}, timeout=15)
print(f'  状态: {r.status_code}')
print(f'  响应: {r.text[:300]}')
print(f'  JSESSIONID: {s.cookies.get("JSESSIONID", "none")}')

# Step 3: 如果登录成功，测试生成申领码
if r.status_code == 200 and r.json().get('status'):
    token = r.json().get('data', {}).get('accessToken', '')
    if not token:
        token = r.json().get('content', {}).get('accessToken', '')
    print(f'\nStep 3: Token={token[:50] if token else "none"}...')
    if token:
        # 测试生成申领码
        p = {'applicantName':'auto-test','applicantCode':'00001','phone':'13800000001',
             'startTime':'2026-08-18 10:00:00','endTime':'2026-08-19 10:00:00',
             'factoryIds':'3','capacity':'16G','format':'FAT32'}
        r2 = s.post(BASE + '/USM/ieg/usbApply/add', json=p, headers={
            'Authorization': token, 'Content-Type': 'application/json;charset=UTF-8',
            'User-Agent': 'Mozilla/5.0', 'Referer': BASE + '/'
        }, timeout=15)
        print(f'  申领测试: {r2.status_code} {r2.text[:300]}')
else:
    print('\n登录失败，检查响应格式')