import { create } from 'zustand';

export interface Operation {
  id: string;
  repository_id: string;
  type: 'plan' | 'apply' | 'destroy';
  status: 'pending' | 'running' | 'success' | 'failed';
  environment: string;
  /** Application name (manifest metadata.name). */
  application: string;
  working_dir: string;
  commit_sha?: string;
  created_at: string;
  log?: string;
  error?: string;
  plan?: string;
  /** Admin "break glass" operation that skipped the TowerCrane policy. */
  emergency: boolean;
  /** Plans: whether there is anything to apply. */
  has_changes: boolean;
  /** Plans waiting for an approver's "yes". */
  awaiting_confirmation: boolean;
  /** What the policy decided about applying a plan. */
  policy_note?: string;
  /** Applies created by confirming a plan. */
  source_plan_id?: string;
}

interface OperationState {
  operations: Operation[];
  currentOperation: Operation | null;
  isLoading: boolean;
  error: string | null;
  
  setOperations: (operations: Operation[]) => void;
  setCurrentOperation: (operation: Operation | null) => void;
  setIsLoading: (isLoading: boolean) => void;
  setError: (error: string | null) => void;
  addOperation: (operation: Operation) => void;
  updateOperation: (operation: Operation) => void;
  clearError: () => void;
}

export const useOperationStore = create<OperationState>((set) => ({
  operations: [],
  currentOperation: null,
  isLoading: false,
  error: null,
  
  setOperations: (operations) => set({ operations }),
  setCurrentOperation: (operation) => set({ currentOperation: operation }),
  setIsLoading: (isLoading) => set({ isLoading }),
  setError: (error) => set({ error }),
  
  addOperation: (operation) => set((state) => ({
    operations: [operation, ...state.operations],
  })),
  
  updateOperation: (operation) => set((state) => ({
    operations: state.operations.map((op) =>
      op.id === operation.id ? operation : op
    ),
    currentOperation: state.currentOperation?.id === operation.id ? operation : state.currentOperation,
  })),
  
  clearError: () => set({ error: null }),
}));
