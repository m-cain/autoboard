export type BoardSnapshot = {
  readonly scrollX: number;
  readonly scrollY: number;
  readonly kanbanScrollLeft: number;
};

type StoredSnapshot = {
  readonly snapshot: BoardSnapshot;
  readyToRestore: boolean;
};
const snapshots = new Map<string, StoredSnapshot>();

export const saveBoardSnapshot = (
  key: string,
  snapshot: BoardSnapshot,
): void => {
  snapshots.set(key, { snapshot, readyToRestore: false });
};
export const boardSnapshot = (key: string): BoardSnapshot | undefined =>
  snapshots.get(key)?.snapshot;
export const prepareBoardSnapshotRestore = (key: string | undefined): void => {
  const stored = key ? snapshots.get(key) : undefined;
  if (stored) stored.readyToRestore = true;
};
export const boardSnapshotToRestore = (
  key: string,
): BoardSnapshot | undefined => {
  const stored = snapshots.get(key);
  return stored?.readyToRestore ? stored.snapshot : undefined;
};
export const clearBoardSnapshot = (key: string): void => {
  snapshots.delete(key);
};
