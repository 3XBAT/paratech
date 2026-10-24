# TASK-118: check of answers with citations (backend; no card UI yet)

Not checked by the agents against the real Hub or app. Deploy the Hub from this branch first (catalog v2).

1. With some recorded lessons that have a summary or lecture text, ask the agent panel a question about a subject's lecture content. No plan is needed (paid read).
2. The panel shows only the short «Ответ с источниками подготовлен» until the card UI task; the answer text and sources are available through the AnswerService binding.
3. Asking the same question again reuses the stored answer (no second charge).
4. A question with no matching lecture text gives no answer and no Hub call.
5. Without a connection the request fails with a retryable message; nothing is stored.
6. A claim whose quote is not found in the lecture is not shown as «из ваших лекций».
7. Long questions (up to 500 characters) are accepted by the search and answer_question tools.
