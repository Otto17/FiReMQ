// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package main

import (
	"encoding/json"

	"FiReMQ/db" // Локальный пакет с БД BadgerDB

	"github.com/dgraph-io/badger/v4"
)

// MoveClient перемещает клиента в новую группу и подгруппу
func MoveClient(clientID, newGroup, newSubgroup string) error {
	return db.DBInstance.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("client:" + clientID))
		if err != nil {
			return err
		}

		var data map[string]string
		err = item.Value(func(val []byte) error {
			return json.Unmarshal(val, &data)
		})
		if err != nil {
			return err
		}

		data["group"] = newGroup
		data["subgroup"] = newSubgroup

		jsonData, err := json.Marshal(data)
		if err != nil {
			return err
		}
		return txn.Set([]byte("client:"+clientID), jsonData)
	})
}

// MoveSelectedClients массово перемещает список клиентов в новую группу и подгруппу
func MoveSelectedClients(clientIDs []string, newGroup, newSubgroup string) ([]string, error) {
	var notFoundIDs []string // Слайс для хранения ID ненайденных клиентов

	err := db.DBInstance.Update(func(txn *badger.Txn) error {
		for _, clientID := range clientIDs {
			key := []byte("client:" + clientID)
			item, err := txn.Get(key)
			if err != nil {
				if err == badger.ErrKeyNotFound {
					// Клиент не найден. Добавляет его ID в список и продолжает
					notFoundIDs = append(notFoundIDs, clientID)
					continue
				}
				// Другая, более серьезная ошибка при чтении
				return err
			}

			var data map[string]string
			if err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &data)
			}); err != nil {
				return err
			}

			data["group"] = newGroup
			data["subgroup"] = newSubgroup

			updated, err := json.Marshal(data)
			if err != nil {
				return err
			}

			if err := txn.Set(key, updated); err != nil {
				return err
			}
		}
		return nil
	})

	// Возвращат список ненайденных ID и возможную ошибку транзакции
	return notFoundIDs, err
}

// GetClientsByGroup возвращает клиентов, соответствующих указанной группе и подгруппе
func GetClientsByGroup(group, subgroup string) ([]ClientInfo, error) {
	var clients []ClientInfo
	err := db.DBInstance.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("client:")
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var data map[string]string
				if err := json.Unmarshal(val, &data); err != nil {
					return err
				}

				if data["group"] == group && data["subgroup"] == subgroup {
					client := ClientInfo{
						Status:    data["status"],
						Name:      data["name"],
						IP:        data["ip"],
						LocalIP:   data["local_ip"],
						ClientID:  data["client_id"],
						Timestamp: data["time_stamp"],
					}
					clients = append(clients, client)
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	return clients, err
}

// GetAllGroupsAndSubgroups возвращает список всех уникальных групп и подгрупп
func GetAllGroupsAndSubgroups() (map[string][]string, error) {
	groups := make(map[string][]string)

	err := db.DBInstance.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("client:") // Фильтрация по префиксу
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var data map[string]string
				if err := json.Unmarshal(val, &data); err != nil {
					return err
				}

				group := data["group"]
				subgroup := data["subgroup"]

				if group != "" {
					groups[group] = append(groups[group], subgroup)
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	// Убирает дубли подгрупп для каждой группы
	for group := range groups {
		groups[group] = unique(groups[group])
	}

	return groups, err
}

// Unique удаляет дубликаты из слайса строк
func unique(slice []string) []string {
	set := make(map[string]struct{})
	result := []string{}
	for _, item := range slice {
		if _, exists := set[item]; !exists {
			set[item] = struct{}{}
			result = append(result, item)
		}
	}
	return result
}
