// Draws the menu bar icon: a monoline "Brn" on one baseline in a square,
// its three curves stepping down like stairs: B's bowls, r as a single
// curve from B's upper bowl onto the top of n, n's arch. Black on transparent,
// for a template image macOS tints to match the menu bar.
//
// Usage: swift scripts/tray-icon.swift [out]   (default: resources/tray.png)
import AppKit

let out = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "resources/tray.png"
// 18 points, the menu bar icon height, at 2x.
let pixels = 36

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
ctx.translateBy(x: (100 - box.width) / 2 - box.minX, y: (100 - box.height) / 2 - box.minY)
ctx.setFillColor(.black)
// One fill each: overlapping outlines filled together could cancel out.
for s in shapes {
    ctx.addPath(s)
    ctx.fillPath()
}

try rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: out))
print("wrote \(out)")
