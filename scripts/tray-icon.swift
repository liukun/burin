// Draws the menu bar icon: a monoline "Brn" on one baseline in a square,
// its three curves stepping down like stairs: B's bowls, r as a single
// curve from B's upper bowl onto the top of n, n's arch. Black on transparent,
// for a template image macOS tints to match the menu bar.
//
// Usage: swift scripts/tray-icon.swift [out [pixels [style]]]
//   out     where to write (default: resources/tray.png)
//   pixels  square size (default: 36, 18 points of menu bar at 2x)
//   style   "white": white mark on transparent, for the README's dark theme
//           "app": white mark on the rounded gradient square of the app icon
//             (swift scripts/tray-icon.swift resources/icon.png 1024 app)
import AppKit

let args = CommandLine.arguments
let out = args.count > 1 ? args[1] : "resources/tray.png"
let pixels = args.count > 2 ? Int(args[2])! : 36
let style = args.count > 3 ? args[3] : "black"
let app = style == "app"
let white = app || style == "white"

// The mark in a 100x100 unit square, y up.
let weight: CGFloat = 11
let base: CGFloat = 4

let stem = CGPath(rect: CGRect(x: 4, y: base, width: weight, height: 80), transform: nil)

let b = CGMutablePath()
b.move(to: CGPoint(x: 9.5, y: 78.5))
b.addLine(to: CGPoint(x: 22, y: 78.5))
b.addArc(center: CGPoint(x: 22, y: 61.75), radius: 16.75, startAngle: .pi / 2, endAngle: -.pi / 2, clockwise: true)
b.addLine(to: CGPoint(x: 9.5, y: 45))
b.move(to: CGPoint(x: 9.5, y: 45))
b.addLine(to: CGPoint(x: 24, y: 45))
b.addArc(center: CGPoint(x: 24, y: 27.25), radius: 17.75, startAngle: .pi / 2, endAngle: -.pi / 2, clockwise: true)
b.addLine(to: CGPoint(x: 9.5, y: 9.5))

// r leaves B's upper bowl level and lands straight down on the middle of
// n's arch. A corner radius near the span (27.5) keeps it mostly curve,
// leaving short straight ends that meet B and n squarely.
let r = CGMutablePath()
r.move(to: CGPoint(x: 43, y: 62))
r.addArc(tangent1End: CGPoint(x: 70.5, y: 62), tangent2End: CGPoint(x: 70.5, y: 34.5), radius: 22)
r.addLine(to: CGPoint(x: 70.5, y: 34.5))

let n = CGMutablePath()
n.move(to: CGPoint(x: 57, y: base))
n.addLine(to: CGPoint(x: 57, y: 22))
n.addArc(center: CGPoint(x: 70.5, y: 22), radius: 13.5, startAngle: .pi, endAngle: 0, clockwise: true)
n.addLine(to: CGPoint(x: 84, y: base))

let strokes = [b, r, n].map {
    $0.copy(strokingWithWidth: weight, lineCap: .butt, lineJoin: .round, miterLimit: 10)
}
let shapes = [stem] + strokes

// Center the mark in the square.
let box = shapes.dropFirst().reduce(stem.boundingBoxOfPath) { $0.union($1.boundingBoxOfPath) }

let rep = NSBitmapImageRep(
    bitmapDataPlanes: nil, pixelsWide: pixels, pixelsHigh: pixels,
    bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
    colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
let ctx = NSGraphicsContext(bitmapImageRep: rep)!.cgContext
ctx.scaleBy(x: CGFloat(pixels) / 100, y: CGFloat(pixels) / 100)
if app {
    // The macOS icon grid: the square fills 824 of 1024 points, its
    // corners rounded at 22.37% of its side, leaving room for the shadow
    // the system draws.
    let side: CGFloat = 100 * 824 / 1024
    let square = CGRect(x: (100 - side) / 2, y: (100 - side) / 2, width: side, height: side)
    ctx.addPath(CGPath(roundedRect: square, cornerWidth: side * 0.2237, cornerHeight: side * 0.2237, transform: nil))
    ctx.clip()
    let colors = [
        CGColor(red: 0.26, green: 0.52, blue: 0.96, alpha: 1),
        CGColor(red: 0.66, green: 0.33, blue: 0.97, alpha: 1),
    ] as CFArray
    let gradient = CGGradient(colorsSpace: CGColorSpaceCreateDeviceRGB(), colors: colors, locations: [0, 1])!
    ctx.drawLinearGradient(gradient, start: CGPoint(x: square.minX, y: square.maxY), end: CGPoint(x: square.maxX, y: square.minY), options: [])
    // The mark takes a bit over half of the square.
    let markScale: CGFloat = 0.52
    ctx.translateBy(x: 50 * (1 - markScale), y: 50 * (1 - markScale))
    ctx.scaleBy(x: markScale, y: markScale)
}
ctx.translateBy(x: (100 - box.width) / 2 - box.minX, y: (100 - box.height) / 2 - box.minY)
ctx.setFillColor(white ? .white : .black)
// One fill each: overlapping outlines filled together could cancel out.
for s in shapes {
    ctx.addPath(s)
    ctx.fillPath()
}

try rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: out))
print("wrote \(out)")
