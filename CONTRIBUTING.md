# Contributing

Thanks for helping out! There are a few ways to pitch in.

## No coding needed

- **Report a problem** or **suggest an idea** with the forms on the [Issues page](../../issues/new/choose).
- **Help other players** by answering questions in Issues.
- **Tell the game projects about the launcher.** Each game is made by its own team, and they're credited on the main page.

## Code changes

1. Fork the repository and make your change.
2. Build and test it (see "For modders and developers" in the README):
   `go test ./...`, then build the Windows exe and try it.
3. Open a pull request saying what it changes and why.

Please keep these rules:

- **Never add game files** (maps, ISOs, MCC files) or links to unofficial copies of them. The launcher only starts games players already have, and installs them from each project's own release page.
- **Keep the launcher free.** Its built-in Halo art is used under Microsoft's [Game Content Usage Rules](https://www.xbox.com/en-US/developers/rules), which don't allow selling it, paywalls or ads, and require the Microsoft notice to stay in the launcher and the README.
- **Don't add Microsoft or Halo logos** as the launcher's own branding.
- Credit the game projects whenever they're mentioned.
- Report security problems privately (see [SECURITY.md](SECURITY.md)), not in public Issues.
