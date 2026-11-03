// «Записать пару» of «Сегодня» (TASK-348): «Записи» opens with the start panel already open. Same one-shot idea as the
// review of the schedule (keepScheduleReview): the request is window state, not part of the route; «Записи» takes it once
// on arrival and the window drops it as soon as the route leaves the page.

/** The request stays only while the route is on «Записи»; any other page drops it, so Alt+← / Alt+→ never open the panel late. */
export function keepStartPanel(wanted: boolean, route: { page: string }): boolean {
  return wanted && route.page === "recordings";
}
