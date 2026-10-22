# TASK-138: check of the file drop (real window)

Not checked by the agents: nothing was started. Do on Windows:

1. Open paratech, go to Предметы. Drag a .pdf from Explorer over the window: the overlay appears, the cursor is not "forbidden".
2. Drop it: a line "<имя>: добавлен во «Входящие»" appears; the file is in «Входящие».
3. Drop the same file again: "уже есть в библиотеке".
4. Drop on different places (sidebar, a dialog, empty area): it works everywhere in the main window.
5. Drop a .exe: refusal text, nothing imported. Drop a file from a network drive (\\server\...): refused.
6. Drop 11 files: whole drop refused ("не больше 10 файлов"). Drop 3 files with one bad: the other two import.
7. The «Добавить файл» button still works.
8. No path appears in the UI or in the log (logs\ folder).
