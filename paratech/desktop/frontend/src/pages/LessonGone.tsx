// The .js extensions let the Node test runner load the compiled screen (test/lessonEntry352.test.mjs renders it).
import { type LessonProblem, lessonScreenTexts } from "../knowledgeView.js";
import { Button } from "../ui/Button.js";
import { Banner } from "../ui/controls.js";

/**
 * The lesson cannot be shown (TASK-352): the reason in the screen's words, «Повторить» only when a repeat may help, and
 * «Назад» to where the student came from. The reason never names a recording or an id.
 */
export function LessonGone(props: { problem: LessonProblem; onRetry: () => void; onBack: () => void }) {
  const retry = props.problem.kind === "retry";
  return (
    <div className="kb-gone">
      <Banner kind="fail" role="alert">
        {props.problem.text}
      </Banner>
      <div className="acts start">
        {retry && <Button onClick={props.onRetry}>{lessonScreenTexts.retry}</Button>}
        <Button variant={retry ? "ghost" : undefined} onClick={props.onBack}>
          {lessonScreenTexts.back}
        </Button>
      </div>
    </div>
  );
}
