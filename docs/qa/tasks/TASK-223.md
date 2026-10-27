# TASK-223 — QA: long agent answers

Needs the Hub with catalog 7 deployed first, then the new desktop build.

1. Ask the agent a simple question («Когда следующая пара?»). The answer is short, 1-3 sentences.
2. Ask for an explanation («Объясни подробно, чем отличается преобразование Фурье от Лапласа»). The answer is long, in paragraphs separated by blank lines; it is not an error and nothing is cut under about 4000 characters.
3. The panel scrolls through the long answer, the input line stays visible, long words wrap.
4. Ask the agent to reply with text containing `<b>test</b>`. It appears as literal text, no bold.
5. After restart the thread shows the answer; the next question works (history keeps only the first 2 KiB of the answer).
6. Hub log of the long answer has no answer text.
