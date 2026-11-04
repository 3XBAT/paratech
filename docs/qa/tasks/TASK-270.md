# TASK-270 — check steps

Start from a cold app (agent panel open on any screen). Without a long chat you see nothing new: the ring appears only after the first replies, and the note is invisible when idle.

1. Open a chat with more than 20 requests (or keep asking in one chat until the ring is high).
2. Send a new request. For a moment a quiet grey line «Уплотнение контекста…» appears under the field (no box, no dots; the dots of the waiting agent are in the thread). The field stays usable and the «Начать новый чат?» line is not shown at that moment.
3. The line disappears; the ring now shows a smaller percent (hover: «Контекст чата: N %»). The agent still remembers the start of the chat (ask about the first request).
4. Scroll the thread while it is compacting: nothing covers the text of the messages, the newest message stays in view when the line appears and disappears.
5. Switch to another chat during compaction: no line there; back — the line is there until it ends.
6. A chat whose old request expired and was restarted may show «Начало разговора агент уже не помнит…» in the ring tooltip and in the hint line under the field, with the button «Новый чат».
