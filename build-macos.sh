#!/bin/bash
set -e

APP_NAME="AmpProxy"
BUNDLE_ID="dev.ampproxy.desktop"
VERSION="${VERSION:-dev}"
BINARY="amp-proxy"
APP_DIR="${APP_NAME}.app"

echo "=== Building ${APP_NAME} macOS app ==="

# 1. Build universal binary (arm64)
echo "→ Compiling..."
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w -X main.version=${VERSION}" -o "${BINARY}" .

# 2. Create .app bundle structure
echo "→ Creating app bundle..."
rm -rf "${APP_DIR}"
mkdir -p "${APP_DIR}/Contents/MacOS"
mkdir -p "${APP_DIR}/Contents/Resources"

# 3. Move binary
mv "${BINARY}" "${APP_DIR}/Contents/MacOS/${BINARY}"

# 4. Create Info.plist
cat > "${APP_DIR}/Contents/Info.plist" << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleDevelopmentRegion</key>
    <string>en</string>
    <key>CFBundleExecutable</key>
    <string>${BINARY}</string>
    <key>CFBundleIconFile</key>
    <string>AppIcon</string>
    <key>CFBundleIdentifier</key>
    <string>${BUNDLE_ID}</string>
    <key>CFBundleInfoDictionaryVersion</key>
    <string>6.0</string>
    <key>CFBundleName</key>
    <string>${APP_NAME}</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>${VERSION}</string>
    <key>CFBundleVersion</key>
    <string>${VERSION}</string>
    <key>LSMinimumSystemVersion</key>
    <string>11.0</string>
    <key>LSUIElement</key>
    <true/>
    <key>NSHighResolutionCapable</key>
    <true/>
    <key>NSSupportsAutomaticGraphicsSwitching</key>
    <true/>
</dict>
</plist>
EOF

# 5. Generate app icon (simple circle icon using sips)
# Create a temporary 512x512 PNG icon
python3 -c "
import struct, zlib

def create_png(w, h, r, g, b):
    def chunk(ctype, data):
        c = ctype + data
        return struct.pack('>I', len(data)) + c + struct.pack('>I', zlib.crc32(c) & 0xffffffff)
    
    rows = []
    cx, cy, rad = w/2, h/2, w/2 - 2
    for y in range(h):
        row = b'\x00'
        for x in range(w):
            dx, dy = x - cx, y - cy
            dist = (dx*dx + dy*dy) ** 0.5
            if dist < rad - 1:
                row += bytes([r, g, b, 255])
            elif dist < rad:
                a = int(255 * (rad - dist))
                row += bytes([r, g, b, a])
            else:
                row += bytes([0, 0, 0, 0])
        rows.append(row)
    
    raw = b''.join(rows)
    return (b'\x89PNG\r\n\x1a\n' +
            chunk(b'IHDR', struct.pack('>IIBBBBB', w, h, 8, 6, 0, 0, 0)) +
            chunk(b'IDAT', zlib.compress(raw)) +
            chunk(b'IEND', b''))

with open('/tmp/amp-icon-512.png', 'wb') as f:
    f.write(create_png(512, 512, 0x34, 0xD3, 0x99))
" 2>/dev/null

# Convert to icns
mkdir -p /tmp/AppIcon.iconset
sips -z 16 16     /tmp/amp-icon-512.png --out /tmp/AppIcon.iconset/icon_16x16.png      2>/dev/null
sips -z 32 32     /tmp/amp-icon-512.png --out /tmp/AppIcon.iconset/icon_16x16@2x.png   2>/dev/null
sips -z 32 32     /tmp/amp-icon-512.png --out /tmp/AppIcon.iconset/icon_32x32.png      2>/dev/null
sips -z 64 64     /tmp/amp-icon-512.png --out /tmp/AppIcon.iconset/icon_32x32@2x.png   2>/dev/null
sips -z 128 128   /tmp/amp-icon-512.png --out /tmp/AppIcon.iconset/icon_128x128.png    2>/dev/null
sips -z 256 256   /tmp/amp-icon-512.png --out /tmp/AppIcon.iconset/icon_128x128@2x.png 2>/dev/null
sips -z 256 256   /tmp/amp-icon-512.png --out /tmp/AppIcon.iconset/icon_256x256.png    2>/dev/null
sips -z 512 512   /tmp/amp-icon-512.png --out /tmp/AppIcon.iconset/icon_256x256@2x.png 2>/dev/null
cp /tmp/amp-icon-512.png /tmp/AppIcon.iconset/icon_512x512.png
iconutil -c icns /tmp/AppIcon.iconset -o "${APP_DIR}/Contents/Resources/AppIcon.icns" 2>/dev/null

# Cleanup
rm -rf /tmp/AppIcon.iconset /tmp/amp-icon-512.png

echo "→ Done!"
echo ""
echo "  ${APP_DIR}  ($(du -sh "${APP_DIR}" | cut -f1))"
echo ""
echo "  Install:  cp -r '${APP_DIR}' /Applications/"
echo "  Run:      open '${APP_DIR}'"
