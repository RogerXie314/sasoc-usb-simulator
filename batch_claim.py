"""批量生成申领码，目标 10 万条"""
import requests, urllib3, time, json, threading, os
urllib3.disable_warnings()

TOKEN = '0dd6503580ff0ae060525a745587832f2b2c3f86ceb5f6a228e468b3a4397806174ed6e9df276dcbdc2c11071cd64e678b4aaa0074004ac488e84a9e5df448749f6d74f895d4c5705e94b433cbcf46ea90e1bea2b214f074189a0db72efab1fd1c54673e02e5cf429a3226b150913979339380335064f9032054eed4cf9f6fcb9f5f094c93ae63730f6740d01706a64a3d02938d67d774010e6969dda1674f615bb195e1305886e6ee6b5ff09825755b50df4434d44143d9bba9d227b9820ef8c9baf8f42e1a6539be788f3c0c472628be621c1df84e247b09d35784439d00278c4dbf72b5159e96bcb35089a5de232017b3293ad73b0d9b46bc741ac13f037f238e32433f0bdcb2007c128692fb72fe0d7b6ad74c6ed53d'
JSESSIONID = '07CBFEC644BFF852939142A018C0858D'
URL = 'https://192.168.123.24:8440/USM/ieg/usbApply/add'
CONCURRENT = 10
TOTAL = 100000

HEADERS = {
    'Authorization': TOKEN,
    'Content-Type': 'application/json;charset=UTF-8',
    'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36',
    'Referer': 'https://192.168.123.24:8440/',
}
COOKIES = {'JSESSIONID': JSESSIONID}

results = []
lock = threading.Lock()
success_count = 0
fail_count = 0
start_time = time.time()

def worker(thread_id):
    global success_count, fail_count
    s = requests.Session()
    s.verify = False
    idx = thread_id
    while True:
        with lock:
            if len(results) >= TOTAL:
                break
            idx = len(results)
        body = {
            'applicantName': 'batch',
            'applicantCode': str(idx + 1).zfill(5),
            'phone': '138' + str(idx % 100000000).zfill(8),
            'startTime': '2026-08-18 10:00:00',
            'endTime': '2026-08-19 10:00:00',
            'factoryIds': '3',
            'capacity': '16G',
            'format': 'FAT32'
        }
        try:
            r = s.post(URL, json=body, headers=HEADERS, cookies=COOKIES, timeout=30)
            if r.status_code == 200:
                data = r.json()
                code = data.get('content', {}).get('applyCode', '')
                with lock:
                    results.append(code)
                    success_count += 1
                if success_count % 100 == 0:
                    elapsed = time.time() - start_time
                    rate = success_count / elapsed if elapsed > 0 else 0
                    eta = (TOTAL - success_count) / rate if rate > 0 else 0
                    print(f'进度: {success_count}/{TOTAL} ({success_count*100/TOTAL:.1f}%) 速率: {rate:.1f}/s ETA: {eta:.0f}s')
            elif r.status_code == 403 and 'KICKOUT' in r.text:
                with lock:
                    results.append(None)
                    fail_count += 1
                # 403 kickout -> 等1秒重试
                time.sleep(1)
                continue
            else:
                with lock:
                    results.append(None)
                    fail_count += 1
                if fail_count % 10 == 0:
                    print(f'失败 {fail_count}: {r.status_code} {r.text[:100]}')
        except Exception as e:
            with lock:
                results.append(None)
                fail_count += 1
            time.sleep(0.5)

# 启动并发线程
threads = []
for i in range(CONCURRENT):
    t = threading.Thread(target=worker, args=(i,))
    t.start()
    threads.append(t)

for t in threads:
    t.join()

elapsed = time.time() - start_time
valid = [r for r in results if r]
print(f'\n完成! 成功: {len(valid)} 失败: {fail_count} 耗时: {elapsed:.0f}s')

# 保存结果
with open('apply_codes.json', 'w') as f:
    json.dump(valid, f)
print(f'已保存 {len(valid)} 条申领码到 apply_codes.json')