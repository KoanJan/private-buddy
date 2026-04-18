from typing import TypeVar, Generic, Type, List, Optional
from sqlalchemy.orm import Session
from fastapi import HTTPException
from pydantic import BaseModel
from app.logger import logger

ModelType = TypeVar("ModelType", bound=object)
CreateSchemaType = TypeVar("CreateSchemaType", bound=BaseModel)
UpdateSchemaType = TypeVar("UpdateSchemaType", bound=BaseModel)


class CRUDBase(Generic[ModelType, CreateSchemaType, UpdateSchemaType]):
    def __init__(self, model: Type[ModelType], entity_name: str):
        self.model = model
        self.entity_name = entity_name

    def get(self, db: Session, id: int) -> Optional[ModelType]:
        logger.info(f"Getting {self.entity_name} {id}")
        obj = db.query(self.model).filter(self.model.id == id).first()
        if obj is None:
            logger.error(f"{self.entity_name} {id} not found")
            raise HTTPException(status_code=404, detail=f"{self.entity_name} not found")
        return obj

    def get_multi(
        self, db: Session, skip: int = 0, limit: int = 100
    ) -> List[ModelType]:
        objs = db.query(self.model).offset(skip).limit(limit).all()
        logger.info(f"Retrieved {len(objs)} {self.entity_name}s")
        return objs

    def create(self, db: Session, obj_in: CreateSchemaType) -> ModelType:
        logger.info(f"Creating {self.entity_name}")
        db_obj = self.model(**obj_in.model_dump())
        db.add(db_obj)
        db.commit()
        db.refresh(db_obj)
        logger.info(f"{self.entity_name} created with ID: {db_obj.id}")
        return db_obj

    def update(
        self, db: Session, db_obj: ModelType, obj_in: UpdateSchemaType
    ) -> ModelType:
        logger.info(f"Updating {self.entity_name} {db_obj.id}")
        update_data = obj_in.model_dump(exclude_unset=True)
        for field, value in update_data.items():
            setattr(db_obj, field, value)
        db.commit()
        db.refresh(db_obj)
        logger.info(f"{self.entity_name} {db_obj.id} updated")
        return db_obj

    def delete(self, db: Session, id: int) -> dict:
        logger.info(f"Deleting {self.entity_name} {id}")
        obj = self.get(db, id)
        db.delete(obj)
        db.commit()
        logger.info(f"{self.entity_name} {id} deleted")
        return {"message": f"{self.entity_name} deleted successfully"}
