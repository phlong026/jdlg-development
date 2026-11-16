'use strict';

const { formatTime } = require('./util');

function isValidDateOnly(year, month, day) {
  if (year < 1 || month < 1 || month > 12 || day < 1) return false;
  const leapYear = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const daysInMonth = [31, leapYear ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return day <= daysInMonth[month - 1];
}

function formatNoteTime(value) {
  if (value === null || value === undefined || value === '') return '';

  const raw = typeof value === 'string' ? value.trim() : value;
  if (raw === '') return '';
  if (typeof raw !== 'string' && !(raw instanceof Date)) return '';

  if (typeof raw === 'string') {
    const dateOnly = /^(\d{4})-(\d{2})-(\d{2})$/.exec(raw);
    if (dateOnly) {
      return isValidDateOnly(Number(dateOnly[1]), Number(dateOnly[2]), Number(dateOnly[3])) ? raw : '';
    }
  }

  // Some iOS runtimes reject RFC3339 nanoseconds; milliseconds preserve all display precision needed here.
  const normalized = typeof raw === 'string'
    ? raw.replace(/(\.\d{3})\d*(Z|[+-]\d{2}:?\d{2})$/, '$1$2')
    : raw;
  const date = normalized instanceof Date ? normalized : new Date(normalized);
  if (Number.isNaN(date.getTime())) return '';

  const [day, time] = formatTime(date).split(' ');
  return day.split('/').join('-') + ' ' + time.slice(0, 5);
}

module.exports = { formatNoteTime };
