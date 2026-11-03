/**
 * Task-list helpers (#170). A task list is a calendar with `kind: 'tasks'`;
 * these are the bits of copy and the default colour that only task lists have.
 * Pure, auto-imported.
 */

/**
 * Colours offered to a new task list, as `#rrggbb`. Each list is told apart by
 * its 8px dot alone, so a new list starts on a colour no list uses yet. The
 * first one is the app's long-standing calendar default.
 */
export const TASK_LIST_COLORS: readonly string[] = [
  '#3788d8',
  '#2f9e44',
  '#e8590c',
  '#ae3ec9',
  '#d6336c',
  '#0c8599',
  '#f08c00',
  '#5c7cfa',
];

/**
 * The colour a new task list starts with: the first of `TASK_LIST_COLORS`
 * that none of `usedColors` has (compared without case), else the palette
 * again from the top, offset by how many are in use.
 */
export function suggestTaskListColor(usedColors: readonly (string | undefined)[]): string {
  const used = new Set(usedColors.filter((c): c is string => !!c).map((c) => c.trim().toLowerCase()));
  const free = TASK_LIST_COLORS.find((c) => !used.has(c));
  return free ?? TASK_LIST_COLORS[usedColors.length % TASK_LIST_COLORS.length]!;
}

/**
 * The question before a task list is deleted. A delete is permanent on the
 * server: every task in it, and every share of it, go with it, and the change
 * log tells each syncing app to drop it. Nothing can be restored.
 */
export function taskListDeleteMessage(name: string): string {
  return `Delete "${name}" and all of its tasks? Everyone it is shared with loses it too, and task apps that sync it remove it. This can't be undone.`;
}
