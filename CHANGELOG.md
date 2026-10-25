# Changelog

## v0.3.5
- New: if a game closes within a few seconds of pressing Play, the launcher explains the most common cause (Windows Security's Controlled folder access) and how to allow the game.
- README: new FAQ entry for games that close right away.
- Halo 2: Project Gravemind is out of closed beta. **Get it** now walks you through its Discord (join the server, then open the download message), and there's a **Community** button for the Discord.

## v0.3.4
- New: the launcher updates itself. When a new version is out, a bar at the top says so. Click **Update now** and it downloads the new version from this repository's Releases page, checks it against the release's SHA-256 checksum, and restarts. Nothing changes unless you click. (Coming from v0.3.3? Download v0.3.4 by hand this one time; after that it's automatic.)
- New background art for Halo: Reach and Halo 3.
- Tall posters now show the whole character instead of being cut off.

## v0.3.3
- New: on first launch the launcher offers to add a desktop shortcut (it only adds one if you say yes). If you move the launcher later, the shortcut follows it.
- Security: "Remove background" can only ever remove the launcher's own background files.
- Security: built-in Windows tools are always run from the Windows folder, never from somewhere else on PATH.
- Security: downloaded archives with reserved Windows file names (like NUL or COM1) are refused.
- Security: the launcher only ever closes programs that run from a game's own folder, and refuses to use a whole drive or shared folders (Desktop, Downloads, Program Files, Windows) as a game folder.
- Security: Project Gravemind is only started if its program is actually named Gravemind.
- Security: stricter checks on game downloads (HTTPS only, size limits) and on the menu's local connection.
- The session key is no longer written to the log file.

## v0.3.2
- The launcher window and taskbar now show the bee icon.

## v0.3.1
- New name: Halo Modding Community Collection (Halo: MCC).

## v0.3.0
- Built-in background art for every game, so the launcher looks right straight away.
- New bee icon.
- Microsoft's Game Content Usage Rules notice in the launcher.

## v0.2.2
- Halo: Combat Evolved now says clearly that you need your own copy of the Xbox ISO file.

## v0.2.1
- Xbox-style A / B / X / Y buttons and easier-to-see keyboard keys along the bottom.

## v0.2.0
- Install games straight from the launcher: it downloads each project's latest release from its own GitHub page and checks it.
- Change any game's background picture.

## v0.1.0
- First version: finds Reach Renaissance, OpenCE, Project Gravemind and Project Reclaimer, shows versions, and plays or switches between them.
