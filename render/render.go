// Package render converts game state into a colored terminal screen.
//
// The world is drawn on a square-pixel grid (Pix×Pix pixels per tile) and
// packed into terminal cells two pixels at a time with the half block '▀'
// (fg = upper pixel, bg = lower pixel) — the finest full-color grid a
// terminal can express. Rendering is a pure function of engine state: no
// I/O, no clocks, no randomness.
package render
