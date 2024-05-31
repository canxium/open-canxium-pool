import Ember from 'ember';

export function formatTx(value) {
  return value[0].substring(0, 11) + "..." + value[0].substring(54);
}

export default Ember.Helper.helper(formatTx);
