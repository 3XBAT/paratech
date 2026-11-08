# Audio fixture of the recording pack (TASK-378)

`chekhov-chelovek-v-futliare-s1-20-70s.mp3`: 50 seconds of a Russian audiobook, read aloud by one voice. The recording pack
(`specs/recording`) imports it through «Импортировать запись» and the real `whisper-cli` transcribes it.

## Source and licence
- Text: Anton Chekhov, «Человек в футляре» (1898; the author died in 1904, public domain).
- Recording: LibriVox volunteers, section 1 of https://librivox.org/chelovekvfutliare-by-anton-chekhov/ . Readers of section 1
  according to LibriVox: Mark Chulsky, tovarisch, Kazbek, Hanna Ponomarenko, Victor Seremet. In this clip one voice speaks.
- File: https://archive.org/download/chelovek_2103_librivox/chelovekvfutliare_01_chekhov_64kb.mp3 (item page
  https://archive.org/details/chelovek_2103_librivox).
- Licence: Public Domain Mark 1.0, http://creativecommons.org/publicdomain/mark/1.0/ (as on the archive.org item).

## How the clip was cut
It is bytes [166890, 566877) of the source file, that is the MPEG frames at 20.0–70.0 s of section 1. The ID3 tag was
removed and the cut is on frame boundaries, so the clip starts with an MPEG frame header. MP3, 64 kbps CBR, mono.

| | bytes | SHA-256 |
|---|---|---|
| clip | 399987 | `8ac7d18f818a282b7734cbeb0d1b8b771243c88d1e8da679a3001cd936cd4ad9` |
| source (5571520 bytes, SHA-1 `1bb57a1bc1ca16493a76093a28d0d5014a90856f`) | 5571520 | `48e6106c2bf3bb2eeb3263cab0bbd88daa068d01ae51f650511afc6063865db4` |

`test/recording.test.ts` checks the size and the SHA-256 of the clip.

## What the clip says
The end of the LibriVox announcement (the first 8 seconds: «…в проекте, пожалуйста, посетите сайт librivox.org»), then the
opening of the story: «На самом краю села Мироносицкого, в сарае старосты Прокофия, расположились на ночлег запоздавшие
охотники. Их было только двое: ветеринарный врач Иван Иваныч и учитель гимназии Буркин. У Ивана Иваныча была довольно
странная, двойная фамилия — Чимша-Гималайский, которая совсем не шла ему, и его во всей губернии звали просто по имени и
отчеству; он жил около города на конном заводе и приехал теперь на охоту, чтобы подышать чистым воздухом».

## What `base-q5_1` makes of it
Checked with the `whisper-cli.exe` and the model (`ggml-base-q5_1.bin`) of the E2E build, language `ru`, VAD on, 4 threads,
CPU only: 14.7 s for the 50 s on the machine of the author. The model gets the words right and the names wrong:

> в проекте, пожалуйста посетите сайт librivox.org. На самом краю в Селомероносецкого, старая старосты про кофе,
> расположились на начале к запоздахшей охотнике. Их было только двое, ветеринарный врач Иван Иванович и учител гимнасий
> Гуркин. У Иван Ивановича была довольно странная двойная фамилия, чемший гималайский, который совершенно не шла ему. И во
> всей кубернии его звали просто по имени очество. Он жил около города на конском заводе, и теперь приехал на охоту, чтобы
> подышать.

Only words that are in Chekhov's text AND that the model wrote correctly are listed below (names, «старосты», «охотники»,
«губернии» are mis-heard and are not). The spec asserts that the transcript has at least two of them, case-insensitively
(`е` and `ё` are the same letter).

Expected words: ветеринарный, врач, фамилия, двойная, странная, охоту, подышать

## Variants made at run time
The app answers a file it has seen before with the EXISTING recording (same SHA-256), so the cancel and restart scenarios import
other bytes: the clip behind an ID3v2 tag with a different title, three times in a row (about 150 s, so the recognition runs
long enough to be stopped). They are written to `<e2e-dir>/fixtures/audio/variants/` by the spec (`specs/recording/driver.ts`,
`src/fixtures/audio.ts`) and never committed.
