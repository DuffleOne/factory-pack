Pick the one for your computer:

| Your computer | Get this |
| --- | --- |
| Windows 10 or 11 | **FactoryPack-Windows.exe**. It isn't signed, so the first time Windows says it protected your PC: click **More info**, then **Run anyway**. |
| macOS 11 or later | **FactoryPack-macOS.zip**. Unzip it and open Factory Pack. |
| Linux, Steam Deck, Bazzite | **FactoryPack-Linux.AppImage**. Allow it to run as a program, then open it. |

The `factory-pack-*` files are the command-line version, for each system. `SHA256SUMS` has a checksum for every file, and each one carries a build attestation: `gh attestation verify <file> --repo DuffleOne/factory-pack` checks it was built here, from this source.

What it sends and what it never touches is all in the [README](https://github.com/DuffleOne/factory-pack#is-it-safe).
