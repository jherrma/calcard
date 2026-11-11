import { describe, expect, it } from 'vitest';
import { suggestTaskListColor, TASK_LIST_COLORS, taskListDeleteMessage } from './taskLists';

// SCOPE (#170 story 27): a new task list's default colour, and the question
// asked before one is deleted (it is permanent on the server).

describe('suggestTaskListColor', () => {
  it('starts on the app default when no list has a colour yet', () => {
    expect(suggestTaskListColor([])).toBe('#3788d8');
    expect(suggestTaskListColor([undefined, ''])).toBe('#3788d8');
  });

  it('takes the first colour no list uses, ignoring case and spaces', () => {
    expect(suggestTaskListColor(['#3788D8'])).toBe('#2f9e44');
    expect(suggestTaskListColor([' #3788d8 ', '#E8590C', '#2F9E44'])).toBe('#ae3ec9');
    expect(suggestTaskListColor(['#123456'])).toBe('#3788d8');
  });

  it('when all are taken, goes round the palette by how many lists there are', () => {
    expect(suggestTaskListColor([...TASK_LIST_COLORS])).toBe(TASK_LIST_COLORS[0]);
    expect(suggestTaskListColor([...TASK_LIST_COLORS, '#000000'])).toBe(TASK_LIST_COLORS[1]);
  });

  it('offers only #rrggbb colours, all different', () => {
    expect(TASK_LIST_COLORS.every((c) => /^#[0-9a-f]{6}$/.test(c))).toBe(true);
    expect(new Set(TASK_LIST_COLORS).size).toBe(TASK_LIST_COLORS.length);
  });
});

describe('taskListDeleteMessage', () => {
  it('names the list and says the tasks, the shares and the synced copies go for good', () => {
    expect(taskListDeleteMessage('Errands')).toBe(
      'Delete "Errands" and all of its tasks? Everyone it is shared with loses it too, and task apps that sync it remove it. This can\'t be undone.',
    );
  });
});
