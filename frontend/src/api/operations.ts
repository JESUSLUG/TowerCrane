import axios from 'axios';
import { Operation } from '../store/operationStore';

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

export interface OperationRequest {
  repository_id: string;
  /** Application name (metadata.name of a .towercrane manifest). */
  application: string;
  /** Admins only: skip the TowerCrane policy ("break glass"). */
  emergency?: boolean;
  /** Emergency apply/destroy: must repeat the Application name. */
  confirm?: string;
}

type PlanRequest = OperationRequest;
type ApplyRequest = OperationRequest;
type DestroyRequest = OperationRequest;

// auth_token is the key used by the auth store (src/store/authStore.ts) when
// persisting the session token, so it must match here too.
function authHeaders() {
  return {
    Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
  };
}

export const operationAPI = {
  async executePlan(data: PlanRequest): Promise<Operation> {
    const response = await axios.post(
      `${API_URL}/repositories/${data.repository_id}/operations/plan`,
      data,
      { headers: authHeaders() }
    );
    return response.data;
  },

  async executeApply(data: ApplyRequest): Promise<Operation> {
    const response = await axios.post(
      `${API_URL}/repositories/${data.repository_id}/operations/apply`,
      data,
      { headers: authHeaders() }
    );
    return response.data;
  },

  async executeDestroy(data: DestroyRequest): Promise<Operation> {
    const response = await axios.post(
      `${API_URL}/repositories/${data.repository_id}/operations/destroy`,
      data,
      { headers: authHeaders() }
    );
    return response.data;
  },

  /** The "yes": apply exactly this saved plan. */
  async confirmPlan(operationId: string): Promise<Operation> {
    const response = await axios.post(`${API_URL}/operations/${operationId}/confirm`, {}, { headers: authHeaders() });
    return response.data;
  },

  async discardPlan(operationId: string): Promise<void> {
    await axios.post(`${API_URL}/operations/${operationId}/discard`, {}, { headers: authHeaders() });
  },

  async getOperation(operationId: string): Promise<Operation> {
    const response = await axios.get(`${API_URL}/operations/${operationId}`, {
      headers: authHeaders(),
    });
    return response.data;
  },

  async listOperations(repositoryId: string, limit?: number, offset?: number): Promise<{ operations: Operation[] }> {
    const params = new URLSearchParams();
    if (limit) params.append('limit', limit.toString());
    if (offset) params.append('offset', offset.toString());

    const response = await axios.get(
      `${API_URL}/repositories/${repositoryId}/operations?${params.toString()}`,
      { headers: authHeaders() }
    );
    return response.data;
  },
};
