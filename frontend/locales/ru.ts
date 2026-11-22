export default { zapretCatalog: {
  title: 'Стратегии Zapret', refresh: 'Обновить', import: 'Импорт', export: 'Экспорт', create: 'Новая стратегия',
  edit: 'Изменить стратегию', clone: 'Копировать стратегию', delete: 'Удалить стратегию', empty: 'Нет управляемых стратегий',
  unavailable: 'Компонент Zapret не готов.', references: 'Используется в', bindings: 'Привязки Direct', direct: 'Direct',
  strategy: 'Выбранная стратегия', status: 'Состояние применения', diagnostic: 'Проверить HTTPS через этот Direct',
  states: { off: 'Выкл.', applied: 'Применено', pending: 'Настройки изменены', blocked: 'Блокировка: активный путь не подтверждён' },
  name: 'Имя', tlsPreset: 'Шаблон TLS/TCP', httpPreset: 'Шаблон HTTP/TCP', visual: 'Профили', raw: 'Параметры nfqws2',
  profile: 'Профиль {index}', profileID: 'ID профиля', ports: 'Порты TCP через запятую', payload: 'Тип данных', split: 'Байт разделения',
  passive: 'Без преобразования', moveUp: 'Поднять профиль', moveDown: 'Опустить профиль', removeProfile: 'Удалить профиль', addProfile: 'Добавить профиль',
  assetScope: 'Допустим только встроенный Lua закреплённого движка. Свои Lua, списки и пути не импортируются.',
  editAffects: 'При изменении профиля защищённые привязки будут перезапущены: {refs}',
  deleteConfirm: 'Удалить «{name}»?', cancel: 'Отмена', save: 'Сохранить', copyName: '{name} (копия)',
  testOK: 'HTTPS через {direct}: {delay} мс', importInvalid: 'Выберите JSON-файл экспорта Zapret размером до 32 КиБ.',
} }
