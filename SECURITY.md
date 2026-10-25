# Security

## Get the launcher only from this page

The only official download is the **Releases** page of this repository. If someone sends you the launcher another way (a Discord file, a mod site, a "fixed" or "unlocked" version), don't run it. Every release lists its SHA-256 checksum in `SHA256SUMS.txt` so you can check your copy.

## What the launcher does and doesn't do

- It **never includes game files**, and it never asks for passwords, Steam logins or payment details.
- It doesn't talk to other players and has no chat, accounts or servers of its own.
- It only installs games from each project's own GitHub release page, over HTTPS, and checks the project's published checksum when there is one.
- Its menu runs on your own PC only (`127.0.0.1`), protected by a random key that changes every time it starts. Websites can't control it.
- It only adds a desktop shortcut if you say yes, and the shortcut only ever points at the launcher itself.
- It only closes programs that run from a game's own folder, and it refuses to use a whole drive or shared folders (Desktop, Downloads, Program Files, Windows) as a game folder.

What it can't protect against: the games are made by separate teams. If one of their GitHub accounts were ever taken over, a bad release could come from there. Some of those projects sign or publish checksums for their releases, and the launcher checks the checksums when they exist.

## Reporting a problem

Found a security issue? **Please don't post it in public Issues.** Use the **Security** tab of this repository, then **Report a vulnerability**, to tell us privately. We'll reply as soon as we can and credit you if you'd like.

Problems with one of the games themselves go to that game's team (links in the README).
