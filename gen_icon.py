import os, tempfile, shutil
from PIL import Image, ImageDraw

def draw_usb(size):
    img = Image.new('RGBA', (size, size), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    s = size / 32.0
    d.rectangle([8*s,4*s,24*s,20*s], fill=(70,130,230,255), outline=(40,80,180,255), width=max(1,int(s)))
    d.rectangle([10*s,6*s,14*s,12*s], fill=(220,220,230,255))
    d.rectangle([18*s,6*s,22*s,12*s], fill=(220,220,230,255))
    d.rectangle([14*s,20*s,18*s,28*s], fill=(70,130,230,255))
    d.ellipse([14.5*s,13*s,17.5*s,16*s], fill=(255,255,255,255))
    d.line([(16*s,15*s),(12*s,12*s)], fill=(255,255,255,255), width=max(1,int(s)))
    d.ellipse([10.5*s,11*s,13.5*s,14*s], fill=(255,255,255,255))
    d.line([(16*s,15*s),(20*s,12*s)], fill=(255,255,255,255), width=max(1,int(s)))
    d.polygon([(20*s,10*s),(22*s,12*s),(20*s,14*s)], fill=(255,255,255,255))
    d.line([(16*s,16*s),(16*s,19*s)], fill=(255,255,255,255), width=max(1,int(s)))
    d.ellipse([14.5*s,26*s,17.5*s,29*s], fill=(70,130,230,255), outline=(40,80,180,255))
    return img

img16 = draw_usb(16)
img32 = draw_usb(32)
img48 = draw_usb(48)

# 先存到临时目录
tmp = os.path.join(tempfile.gettempdir(), 'usb.ico')
img32.save(tmp, format='ICO', sizes=[(16,16),(32,32),(48,48)])

# 复制到目标（用Python处理中文路径）
target = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'usb.ico')
shutil.copy2(tmp, target)

with open(os.path.join(os.path.dirname(os.path.abspath(__file__)), 'gen_result.txt'), 'w') as f:
    f.write(f"tmp={tmp}\n")
    f.write(f"target={target}\n")
    f.write(f"size={os.path.getsize(target)}\n")
    f.write("OK\n")
