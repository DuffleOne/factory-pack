Pick the one for your computer:

| Your computer | Get this |
| --- | --- |
| Windows 10 or 11 | **FactoryPack-Windows.exe**. It isn't signed, so the first time Windows says it protected your PC: click **More info**, then **Run anyway**. |
| macOS 11 or later | **FactoryPack-macOS.zip**. Unzip it and open Factory Pack. |
| Linux, Steam Deck, Bazzite | **FactoryPack-Linux.AppImage**. Allow it to run as a program, then open it. |

The `factory-pack-*` files are the command-line version, for each system. `SHA256SUMS` has a checksum for every file. The Windows and Linux files carry a build attestation, so `gh attestation verify FactoryPack-Windows.exe --repo DuffleOne/factory-pack` checks a download was built here, from this source; the Mac ones are signed with a Developer ID and notarised by Apple.

What it sends and what it never touches is all in the [README](https://github.com/DuffleOne/factory-pack#is-it-safe).
