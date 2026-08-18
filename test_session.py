import requests, urllib3, time
urllib3.disable_warnings()
TOKEN = '0dd6503580ff0ae060525a745587832f2b2c3f86ceb5f6a228e468b3a4397806174ed6e9df276dcbdc2c11071cd64e678b4aaa0074004ac488e84a9e5df448749f6d74f895d4c5705e94b433cbcf46ea90e1bea2b214f074189a0db72efab1fd1c54673e02e5cf429a3226b150913979339380335064f9032054eed4cf9f6fcb9f5f094c93ae63730f6740d01706a64a3d02938d67d774010e6969dda1674f615bb195e1305886e6ee6b5ff09825755b50df4434d44143d9bba9d227b9820ef8c9baf8f42e1a6539be788f3c0c472628be621c1df84e247b09d35784439d00278c4dbf72b5159e96bcb35089a5de232017b3293ad73b0d9b46bc741ac13f037f238e32433f0bdcb2007c128692fb72fe0d7b6ad74c6ed53d'
H = {'Authorization': TOKEN, 'Content-Type': 'application/json;charset=UTF-8', 'User-Agent': 'Mozilla/5.0', 'Referer': 'https://192.168.123.24:8440/'}
URL = 'https://192.168.123.24:8440/USM/ieg/usbApply/add'
P = {'applicantName':'batch','applicantCode':'00001','phone':'13800000001','startTime':'2026-08-18 10:00:00','endTime':'2026-08-19 10:00:00','factoryIds':'3','capacity':'16G','format':'FAT32'}

s = requests.Session()
s.verify = False

# Step 1: no cookie, trigger kickout, get new JSESSIONID
print('Step 1: no cookie, trigger kickout...')
r1 = s.post(URL, json=P, headers=H, timeout=30)
print(f'  status: {r1.status_code} cookies: {dict(s.cookies)}')
print(f'  Set-Cookie: {r1.headers.get("Set-Cookie", "")[:200]}')

time.sleep(1)

# Step 2: with new JSESSIONID
print('Step 2: with new JSESSIONID...')
r2 = s.post(URL, json=P, headers=H, timeout=30)
print(f'  status: {r2.status_code} body: {r2.text[:300]}')