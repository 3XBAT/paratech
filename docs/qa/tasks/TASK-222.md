# TASK-222 — QA

1. Install the new build over the old one with the installer: the speech component is ready (the installer ships the VAD file `ggml-silero-v5.1.2.bin`). If the component was installed in the app by an older version, the page shows it as damaged: install it again (~12.5 MB).
2. Record or take a 5+ minute lecture with pauses, recognize it. Compare the time with the estimate shown before the start: it should be close (the estimate is about twice larger than before).
3. In the transcript, a long pause in the lecture starts a new paragraph, and the time codes of passages match the recording (click a time code and listen).
4. The live preview during recognition fills in order, no jumps back.
5. After the update the "speed" of models is measured again (old measurements are cleared).
