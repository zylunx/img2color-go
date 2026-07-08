package cache

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoStore struct {
	client     *mongo.Client
	collection *mongo.Collection
}

type mongoDoc struct {
	ID       string      `bson:"_id"`
	ExpireAt time.Time   `bson:"expireAt"`
	Value    interface{} `bson:"value"`
}

func NewMongoStore(ctx context.Context, uri, db string) (*MongoStore, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return nil, err
	}
	return &MongoStore{client: client, collection: client.Database(db).Collection("image_color_cache")}, nil
}

func (m *MongoStore) Get(ctx context.Context, key string, dst interface{}) error {
	var doc mongoDoc
	if err := m.collection.FindOne(ctx, bson.M{"_id": key, "expireAt": bson.M{"$gt": time.Now()}}).Decode(&doc); err != nil {
		if err == mongo.ErrNoDocuments {
			return ErrMiss
		}
		return err
	}
	return copyJSON(dst, doc.Value)
}

func (m *MongoStore) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	_, err := m.collection.UpdateOne(ctx, bson.M{"_id": key}, bson.M{"$set": bson.M{"value": value, "expireAt": time.Now().Add(ttl)}}, options.Update().SetUpsert(true))
	return err
}

func (m *MongoStore) Close(ctx context.Context) error {
	return m.client.Disconnect(ctx)
}
